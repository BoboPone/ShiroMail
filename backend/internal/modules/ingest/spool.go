package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"shiro-email/backend/internal/modules/mailbox"
)

var ErrSpoolItemNotFound = errors.New("spool item not found")

const (
	SpoolStatusPending    = "pending"
	SpoolStatusProcessing = "processing"
	SpoolStatusCompleted  = "completed"
	SpoolStatusFailed     = "failed"
	DefaultSpoolMaxTries  = 3
)

type SpoolItem struct {
	ID               uint64
	MailFrom         string
	Recipients       []string
	TargetMailboxIDs []uint64
	RawMessage       []byte
	Status           string
	ErrorMessage     string
	AttemptCount     int
	MaxAttempts      int
	CreatedAt        time.Time
	UpdatedAt        time.Time
	NextAttemptAt    time.Time
	ProcessedAt      *time.Time
}

type SpoolRepository interface {
	Enqueue(ctx context.Context, item SpoolItem) (SpoolItem, error)
	ClaimNext(ctx context.Context) (SpoolItem, error)
	MarkCompleted(ctx context.Context, id uint64) error
	MarkFailed(ctx context.Context, id uint64, errorMessage string) error
	List(ctx context.Context) ([]SpoolItem, error)
	Retry(ctx context.Context, id uint64) (SpoolItem, error)
}

type MemorySpoolRepository struct {
	mu     sync.Mutex
	nextID uint64
	items  []SpoolItem
}

func NewMemorySpoolRepository() *MemorySpoolRepository {
	return &MemorySpoolRepository{nextID: 1}
}

func (r *MemorySpoolRepository) Enqueue(_ context.Context, item SpoolItem) (SpoolItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	item.ID = r.nextID
	r.nextID++
	item.Status = SpoolStatusPending
	if item.MaxAttempts <= 0 {
		item.MaxAttempts = DefaultSpoolMaxTries
	}
	item.CreatedAt = now
	item.UpdatedAt = now
	item.NextAttemptAt = now
	item.Recipients = append([]string{}, item.Recipients...)
	item.TargetMailboxIDs = append([]uint64{}, item.TargetMailboxIDs...)
	item.RawMessage = cloneBytes(item.RawMessage)
	r.items = append(r.items, item)
	return cloneSpoolItem(item), nil
}

func (r *MemorySpoolRepository) ClaimNext(_ context.Context) (SpoolItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for index := range r.items {
		if r.items[index].Status != SpoolStatusPending {
			continue
		}
		if r.items[index].NextAttemptAt.After(time.Now().UTC()) {
			continue
		}
		r.items[index].Status = SpoolStatusProcessing
		r.items[index].AttemptCount++
		r.items[index].UpdatedAt = time.Now().UTC()
		return cloneSpoolItem(r.items[index]), nil
	}
	return SpoolItem{}, ErrSpoolItemNotFound
}

func (r *MemorySpoolRepository) MarkCompleted(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for index := range r.items {
		if r.items[index].ID != id {
			continue
		}
		now := time.Now().UTC()
		r.items[index].Status = SpoolStatusCompleted
		r.items[index].UpdatedAt = now
		r.items[index].NextAttemptAt = time.Time{}
		r.items[index].ProcessedAt = &now
		r.items[index].ErrorMessage = ""
		return nil
	}
	return ErrSpoolItemNotFound
}

func (r *MemorySpoolRepository) MarkFailed(_ context.Context, id uint64, errorMessage string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for index := range r.items {
		if r.items[index].ID != id {
			continue
		}
		now := time.Now().UTC()
		r.items[index].UpdatedAt = now
		r.items[index].ErrorMessage = errorMessage
		if r.items[index].AttemptCount < r.items[index].MaxAttempts {
			r.items[index].Status = SpoolStatusPending
			r.items[index].NextAttemptAt = now.Add(5 * time.Second)
			return nil
		}
		r.items[index].Status = SpoolStatusFailed
		r.items[index].NextAttemptAt = time.Time{}
		return nil
	}
	return ErrSpoolItemNotFound
}

func (r *MemorySpoolRepository) List(_ context.Context) ([]SpoolItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	items := make([]SpoolItem, 0, len(r.items))
	for _, item := range r.items {
		items = append(items, cloneSpoolItem(item))
	}
	return items, nil
}

func (r *MemorySpoolRepository) Retry(_ context.Context, id uint64) (SpoolItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for index := range r.items {
		if r.items[index].ID != id {
			continue
		}
		now := time.Now().UTC()
		r.items[index].Status = SpoolStatusPending
		r.items[index].ErrorMessage = ""
		r.items[index].UpdatedAt = now
		r.items[index].NextAttemptAt = now
		r.items[index].ProcessedAt = nil
		r.items[index].AttemptCount = 0
		return cloneSpoolItem(r.items[index]), nil
	}
	return SpoolItem{}, ErrSpoolItemNotFound
}

func cloneSpoolItem(item SpoolItem) SpoolItem {
	item.Recipients = append([]string{}, item.Recipients...)
	item.TargetMailboxIDs = append([]uint64{}, item.TargetMailboxIDs...)
	item.RawMessage = cloneBytes(item.RawMessage)
	return item
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}

func (s *DirectService) SetSpoolRepository(repo SpoolRepository) {
	s.spool = repo
}

func (s *DirectService) ProcessNextSpool(ctx context.Context) error {
	if s.spool == nil {
		return ErrSpoolItemNotFound
	}

	item, err := s.spool.ClaimNext(ctx)
	if err != nil {
		return err
	}

	targets, err := s.resolveSpoolTargets(ctx, item.TargetMailboxIDs)
	if err != nil {
		_ = s.spool.MarkFailed(ctx, item.ID, err.Error())
		return err
	}

	if _, err := s.processRawToTargets(ctx, InboundEnvelope{
		MailFrom:   item.MailFrom,
		Recipients: item.Recipients,
	}, item.RawMessage, targets); err != nil {
		_ = s.spool.MarkFailed(ctx, item.ID, err.Error())
		return err
	}

	if err := s.spool.MarkCompleted(ctx, item.ID); err != nil {
		return err
	}
	return nil
}

func (s *DirectService) PeekNextSpool(ctx context.Context) (SpoolItem, error) {
	if s.spool == nil {
		return SpoolItem{}, ErrSpoolItemNotFound
	}
	return s.spool.ClaimNext(ctx)
}

func (s *DirectService) ListSpool(ctx context.Context) ([]SpoolItem, error) {
	if s.spool == nil {
		return []SpoolItem{}, nil
	}
	return s.spool.List(ctx)
}

func (s *DirectService) RetrySpoolItem(ctx context.Context, id uint64) (SpoolItem, error) {
	if s.spool == nil {
		return SpoolItem{}, ErrSpoolItemNotFound
	}
	return s.spool.Retry(ctx, id)
}

func (s *DirectService) resolveSpoolTargets(ctx context.Context, ids []uint64) ([]mailbox.Mailbox, error) {
	if len(ids) == 0 {
		return nil, mailbox.ErrMailboxNotFound
	}
	targets := make([]mailbox.Mailbox, 0, len(ids))
	for _, id := range ids {
		item, err := s.mailboxes.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		targets = append(targets, item)
	}
	return dedupeTargets(targets), nil
}

func marshalUint64s(values []uint64) ([]byte, error) {
	return json.Marshal(values)
}

func unmarshalUint64s(raw []byte) ([]uint64, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var values []uint64
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode target mailbox ids: %w", err)
	}
	return values, nil
}
