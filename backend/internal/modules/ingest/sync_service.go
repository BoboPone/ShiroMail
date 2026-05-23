package ingest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"shiro-email/backend/internal/modules/mailbox"
)

var ErrMessageNotFound = errors.New("message not found")

const LegacySourceKind = "legacy-sync"

type StoredMessage struct {
	ID               uint64              `json:"id"`
	MailboxID        uint64              `json:"mailboxId"`
	LegacyMailboxKey string              `json:"legacyMailboxKey"`
	LegacyMessageKey string              `json:"legacyMessageKey"`
	SourceKind       string              `json:"sourceKind"`
	SourceMessageID  string              `json:"sourceMessageId"`
	MailboxAddress   string              `json:"mailboxAddress"`
	FromAddr         string              `json:"fromAddr"`
	ToAddr           string              `json:"toAddr"`
	Subject          string              `json:"subject"`
	TextPreview      string              `json:"textPreview"`
	HTMLPreview      string              `json:"htmlPreview"`
	TextBody         string              `json:"textBody"`
	HTMLBody         string              `json:"htmlBody"`
	Headers          map[string][]string `json:"headers"`
	RawStorageKey    string              `json:"rawStorageKey"`
	HasAttachments   bool                `json:"hasAttachments"`
	SizeBytes        int64               `json:"sizeBytes"`
	IsRead           bool                `json:"isRead"`
	IsDeleted        bool                `json:"isDeleted"`
	ReceivedAt       time.Time           `json:"receivedAt"`
	Attachments      []StoredAttachment  `json:"attachments"`
}

type StoredAttachment struct {
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	StorageKey  string `json:"storageKey"`
	SizeBytes   int64  `json:"sizeBytes"`
}

type StoredInboundMessage struct {
	SourceKind      string
	SourceMessageID string
	MailboxAddress  string
	FromAddr        string
	ToAddr          string
	Subject         string
	TextPreview     string
	HTMLPreview     string
	TextBody        string
	HTMLBody        string
	Headers         map[string][]string
	RawStorageKey   string
	HasAttachments  bool
	SizeBytes       int64
	ReceivedAt      time.Time
	Attachments     []StoredAttachment
}

type MessageRepository interface {
	UpsertFromLegacySync(ctx context.Context, mailboxID uint64, mailboxName string, parsed ParsedMessage) error
	StoreInbound(ctx context.Context, mailboxID uint64, item StoredInboundMessage) error
	ListByMailboxID(ctx context.Context, mailboxID uint64) ([]StoredMessage, error)
	SearchByMailboxID(ctx context.Context, mailboxID uint64, query string) ([]StoredMessage, error)
}

type LegacySyncService struct {
	client      LegacyMailSyncClient
	messageRepo MessageRepository
}

type MemoryMessageRepository struct {
	mu      sync.RWMutex
	nextID  uint64
	records map[string]StoredMessage
}

func NewLegacySyncService(client LegacyMailSyncClient, messageRepo MessageRepository) *LegacySyncService {
	return &LegacySyncService{
		client:      client,
		messageRepo: messageRepo,
	}
}

func NewMemoryMessageRepository() *MemoryMessageRepository {
	return &MemoryMessageRepository{
		nextID:  1,
		records: map[string]StoredMessage{},
	}
}

func (s *LegacySyncService) SyncMailbox(ctx context.Context, target mailbox.Mailbox) error {
	headers, err := s.client.ListMailbox(ctx, target.LocalPart)
	if err != nil {
		return err
	}

	for _, header := range headers {
		raw, err := s.client.GetMessage(ctx, target.LocalPart, header.ID)
		if err != nil {
			return err
		}
		if raw.Mailbox == "" {
			raw.Mailbox = target.LocalPart
		}
		if raw.ID == "" {
			raw.ID = header.ID
		}

		parsed := ParseLegacyRawMessage(raw)
		if err := s.messageRepo.UpsertFromLegacySync(ctx, target.ID, target.LocalPart, parsed); err != nil {
			return err
		}
	}

	return nil
}

func (r *MemoryMessageRepository) UpsertFromLegacySync(_ context.Context, mailboxID uint64, mailboxName string, parsed ParsedMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := sourceMessageKey(mailboxID, LegacySourceKind, parsed.LegacyMessageKey)
	record, exists := r.records[key]
	if !exists {
		record.ID = r.nextID
		r.nextID++
	}

	record.MailboxID = mailboxID
	record.LegacyMailboxKey = mailboxName
	record.LegacyMessageKey = parsed.LegacyMessageKey
	record.SourceKind = LegacySourceKind
	record.SourceMessageID = parsed.LegacyMessageKey
	record.MailboxAddress = parsed.ToAddr
	record.FromAddr = parsed.FromAddr
	record.ToAddr = parsed.ToAddr
	record.Subject = parsed.Subject
	record.TextPreview = parsed.TextPreview
	record.HTMLPreview = parsed.HTMLPreview
	record.TextBody = parsed.TextPreview
	record.HTMLBody = parsed.HTMLPreview
	record.Headers = cloneHeaderMap(parsed.Header)
	record.RawStorageKey = ""
	record.HasAttachments = len(parsed.Attachments) > 0
	record.SizeBytes = parsed.SizeBytes
	record.IsRead = parsed.IsRead
	record.IsDeleted = false
	record.ReceivedAt = parsed.ReceivedAt
	record.Attachments = cloneAttachments(parsed.Attachments)
	r.records[key] = record
	return nil
}

func (r *MemoryMessageRepository) StoreInbound(_ context.Context, mailboxID uint64, item StoredInboundMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := inboundSourceMessageKey(mailboxID, item.MailboxAddress, item.SourceKind, item.SourceMessageID)
	record, exists := r.records[key]
	if !exists {
		record.ID = r.nextID
		r.nextID++
	}

	record.MailboxID = mailboxID
	record.SourceKind = item.SourceKind
	record.SourceMessageID = item.SourceMessageID
	record.MailboxAddress = item.MailboxAddress
	record.FromAddr = item.FromAddr
	record.ToAddr = item.ToAddr
	record.Subject = item.Subject
	record.TextPreview = item.TextPreview
	record.HTMLPreview = item.HTMLPreview
	record.TextBody = item.TextBody
	record.HTMLBody = item.HTMLBody
	record.Headers = cloneHeaderMap(item.Headers)
	record.RawStorageKey = item.RawStorageKey
	record.HasAttachments = item.HasAttachments
	record.SizeBytes = item.SizeBytes
	record.IsDeleted = false
	record.ReceivedAt = item.ReceivedAt
	record.Attachments = cloneStoredAttachments(item.Attachments)
	r.records[key] = record
	return nil
}

func (r *MemoryMessageRepository) ListByMailboxID(_ context.Context, mailboxID uint64) ([]StoredMessage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]StoredMessage, 0)
	for _, record := range r.records {
		if record.MailboxID == mailboxID && !record.IsDeleted {
			items = append(items, record)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].ReceivedAt.Equal(items[j].ReceivedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].ReceivedAt.After(items[j].ReceivedAt)
	})
	return items, nil
}

func (r *MemoryMessageRepository) SearchByMailboxID(_ context.Context, mailboxID uint64, query string) ([]StoredMessage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lowerQuery := strings.ToLower(query)
	items := make([]StoredMessage, 0)
	for _, record := range r.records {
		if record.MailboxID != mailboxID || record.IsDeleted {
			continue
		}
		if strings.Contains(strings.ToLower(record.FromAddr), lowerQuery) ||
			strings.Contains(strings.ToLower(record.Subject), lowerQuery) ||
			strings.Contains(strings.ToLower(record.TextPreview), lowerQuery) {
			items = append(items, record)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].ReceivedAt.After(items[j].ReceivedAt)
	})
	return items, nil
}

func (r *MemoryMessageRepository) GetByMailboxAndID(_ context.Context, mailboxID uint64, messageID uint64) (StoredMessage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, record := range r.records {
		if record.MailboxID == mailboxID && record.ID == messageID {
			return record, nil
		}
	}
	return StoredMessage{}, ErrMessageNotFound
}

func (r *MemoryMessageRepository) SoftDeleteByMailboxIDs(_ context.Context, mailboxIDs []uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(mailboxIDs) == 0 {
		return nil
	}

	targets := make(map[uint64]struct{}, len(mailboxIDs))
	for _, id := range mailboxIDs {
		targets[id] = struct{}{}
	}

	for key, record := range r.records {
		if _, ok := targets[record.MailboxID]; ok {
			record.IsDeleted = true
			r.records[key] = record
		}
	}
	return nil
}

func (r *MemoryMessageRepository) RestoreByMailboxAddress(_ context.Context, mailboxAddress string, mailboxID uint64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	address := strings.ToLower(strings.TrimSpace(mailboxAddress))
	if address == "" {
		return 0, nil
	}

	restored := 0
	for key, record := range r.records {
		if !strings.EqualFold(record.MailboxAddress, address) && !strings.EqualFold(record.ToAddr, address) {
			continue
		}

		delete(r.records, key)
		record.MailboxID = mailboxID
		record.MailboxAddress = address
		record.IsDeleted = false
		r.records[sourceMessageKey(mailboxID, record.SourceKind, record.SourceMessageID)] = record
		restored++
	}
	return restored, nil
}

func (r *MemoryMessageRepository) CountToday(_ context.Context) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	count := 0
	for _, record := range r.records {
		if record.IsDeleted {
			continue
		}
		yearA, monthA, dayA := record.ReceivedAt.Date()
		yearB, monthB, dayB := now.Date()
		if yearA == yearB && monthA == monthB && dayA == dayB {
			count++
		}
	}
	return count
}

func cloneAttachments(items []ParsedAttachment) []StoredAttachment {
	cloned := make([]StoredAttachment, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, StoredAttachment{
			FileName:    item.FileName,
			ContentType: item.ContentType,
			StorageKey:  item.StorageKey,
			SizeBytes:   item.SizeBytes,
		})
	}
	return cloned
}

func cloneStoredAttachments(items []StoredAttachment) []StoredAttachment {
	cloned := make([]StoredAttachment, 0, len(items))
	cloned = append(cloned, items...)
	return cloned
}

func cloneHeaderMap(headers map[string][]string) map[string][]string {
	cloned := make(map[string][]string, len(headers))
	for key, values := range headers {
		copied := make([]string, len(values))
		copy(copied, values)
		cloned[key] = copied
	}
	return cloned
}

func sourceMessageKey(mailboxID uint64, sourceKind string, sourceMessageID string) string {
	return fmt.Sprintf("%d:%s:%s", mailboxID, sourceKind, sourceMessageID)
}

func inboundSourceMessageKey(mailboxID uint64, mailboxAddress string, sourceKind string, sourceMessageID string) string {
	if mailboxID != 0 {
		return sourceMessageKey(mailboxID, sourceKind, sourceMessageID)
	}
	return fmt.Sprintf("orphan:%s:%s:%s", strings.ToLower(strings.TrimSpace(mailboxAddress)), sourceKind, sourceMessageID)
}
