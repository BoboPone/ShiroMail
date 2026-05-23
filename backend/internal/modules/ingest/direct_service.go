package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"shiro-email/backend/internal/modules/mailbox"
)

type InboundStore interface {
	StoreInbound(ctx context.Context, mailboxID uint64, item StoredInboundMessage) error
}

type ForwardingCallback func(ctx context.Context, mailboxAddress string, forwardTo string, subject string, rawBytes []byte)

type DeliveryCallback func(mailboxUserID uint64, mailboxID uint64, mailboxAddress string, subject string)
type CatchAllRecipientResolver func(ctx context.Context, address string) (mailbox.Mailbox, error)

type DirectService struct {
	mailboxes             mailbox.Repository
	store                 InboundStore
	storage               FileStorage
	onDelivery            DeliveryCallback
	onForwarding          ForwardingCallback
	catchAllResolver      CatchAllRecipientResolver
	inboundPolicyProvider InboundPolicyProvider
	spool                 SpoolRepository
}

func NewDirectService(mailboxes mailbox.Repository, store InboundStore, storage FileStorage) *DirectService {
	return &DirectService{
		mailboxes: mailboxes,
		store:     store,
		storage:   storage,
	}
}

func (s *DirectService) SetDeliveryCallback(cb DeliveryCallback) {
	s.onDelivery = cb
}

func (s *DirectService) SetForwardingCallback(cb ForwardingCallback) {
	s.onForwarding = cb
}

func (s *DirectService) SetCatchAllRecipientResolver(resolver CatchAllRecipientResolver) {
	s.catchAllResolver = resolver
}

func (s *DirectService) SetInboundPolicyProvider(provider InboundPolicyProvider) {
	s.inboundPolicyProvider = provider
}

func (s *DirectService) ResolveRecipient(ctx context.Context, address string) (mailbox.Mailbox, error) {
	normalized := strings.ToLower(strings.TrimSpace(address))
	target, err := s.mailboxes.FindActiveByAddress(ctx, normalized)
	if err == nil {
		return target, nil
	}
	if !errors.Is(err, mailbox.ErrMailboxNotFound) || s.catchAllResolver == nil {
		return mailbox.Mailbox{}, err
	}
	return s.catchAllResolver(ctx, normalized)
}

func (s *DirectService) Deliver(ctx context.Context, env InboundEnvelope, source io.Reader) (StoredInboundMessage, error) {
	targets, err := s.resolveTargets(ctx, env.Recipients)
	if err != nil {
		return StoredInboundMessage{}, err
	}
	return s.deliverToTargets(ctx, env, source, targets)
}

func (s *DirectService) DeliverResolved(ctx context.Context, env InboundEnvelope, source io.Reader, targets []mailbox.Mailbox) (StoredInboundMessage, error) {
	if len(targets) == 0 {
		return StoredInboundMessage{}, mailbox.ErrMailboxNotFound
	}
	return s.deliverToTargets(ctx, env, source, dedupeTargets(targets))
}

func (s *DirectService) resolveTargets(ctx context.Context, recipients []string) ([]mailbox.Mailbox, error) {
	targets := make([]mailbox.Mailbox, 0, len(recipients))
	for _, recipient := range recipients {
		target, err := s.ResolveRecipient(ctx, recipient)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return dedupeTargets(targets), nil
}

func dedupeTargets(targets []mailbox.Mailbox) []mailbox.Mailbox {
	if len(targets) <= 1 {
		return targets
	}

	seen := make(map[string]struct{}, len(targets))
	filtered := make([]mailbox.Mailbox, 0, len(targets))
	for _, target := range targets {
		key := targetDedupeKey(target)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		filtered = append(filtered, target)
	}
	return filtered
}

func targetDedupeKey(target mailbox.Mailbox) string {
	if target.ID != 0 {
		return fmt.Sprintf("id:%d", target.ID)
	}
	return "address:" + strings.ToLower(strings.TrimSpace(target.Address))
}

func (s *DirectService) deliverToTargets(ctx context.Context, env InboundEnvelope, source io.Reader, targets []mailbox.Mailbox) (StoredInboundMessage, error) {
	if len(env.Recipients) == 0 {
		return StoredInboundMessage{}, fmt.Errorf("at least one recipient is required")
	}
	if len(targets) == 0 {
		return StoredInboundMessage{}, mailbox.ErrMailboxNotFound
	}

	const maxMessageSize = 10 * 1024 * 1024 // 10 MB
	rawBytes, err := io.ReadAll(io.LimitReader(source, maxMessageSize+1))
	if err != nil {
		return StoredInboundMessage{}, err
	}
	if len(rawBytes) > maxMessageSize {
		return StoredInboundMessage{}, fmt.Errorf("message exceeds maximum size of %d bytes", maxMessageSize)
	}

	parsed, err := ParseInboundMessage(env, bytes.NewReader(rawBytes))
	if err != nil {
		return StoredInboundMessage{}, err
	}
	policy, err := s.resolveInboundPolicy(ctx, targets)
	if err != nil {
		return StoredInboundMessage{}, err
	}
	if err := validateInboundMessageAttachments(parsed, policy); err != nil {
		return StoredInboundMessage{}, err
	}
	if s.spool != nil && allTargetsHaveMailboxIDs(targets) {
		queued, err := s.spool.Enqueue(ctx, SpoolItem{
			MailFrom:         env.MailFrom,
			Recipients:       append([]string{}, env.Recipients...),
			TargetMailboxIDs: mailboxIDsFromTargets(targets),
			RawMessage:       rawBytes,
		})
		if err != nil {
			return StoredInboundMessage{}, err
		}
		return StoredInboundMessage{
			SourceKind:      "smtp-spool",
			SourceMessageID: fmt.Sprintf("spool-%d", queued.ID),
			MailboxAddress:  targets[0].Address,
			FromAddr:        env.MailFrom,
			ToAddr:          strings.Join(env.Recipients, ","),
		}, nil
	}

	return s.storeParsedToTargets(ctx, env, parsed, targets)
}

func (s *DirectService) processRawToTargets(ctx context.Context, env InboundEnvelope, raw []byte, targets []mailbox.Mailbox) (StoredInboundMessage, error) {
	parsed, err := ParseInboundMessage(env, bytes.NewReader(raw))
	if err != nil {
		return StoredInboundMessage{}, err
	}
	return s.storeParsedToTargets(ctx, env, parsed, targets)
}

func (s *DirectService) storeParsedToTargets(ctx context.Context, env InboundEnvelope, parsed InboundMessage, targets []mailbox.Mailbox) (StoredInboundMessage, error) {
	sourceMessageID := buildSourceMessageID(parsed.RawBytes)
	baseReceivedAt := parsed.ReceivedAt
	if baseReceivedAt.IsZero() {
		baseReceivedAt = time.Now().UTC()
	}

	deliveredTo := targets[0]
	lastItem := StoredInboundMessage{}
	for _, target := range targets {
		rawStorageKey, err := s.storage.StoreRaw(ctx, target.Address, sourceMessageID, parsed.RawBytes)
		if err != nil {
			return StoredInboundMessage{}, err
		}

		attachments := make([]StoredAttachment, 0, len(parsed.Attachments))
		for index, attachment := range parsed.Attachments {
			stored, err := s.storage.StoreAttachment(ctx, target.Address, sourceMessageID, attachment, index)
			if err != nil {
				return StoredInboundMessage{}, err
			}
			attachments = append(attachments, stored)
		}

		item := StoredInboundMessage{
			SourceKind:      "smtp",
			SourceMessageID: sourceMessageID,
			MailboxAddress:  target.Address,
			FromAddr:        firstNonEmpty(parsed.FromAddr, env.MailFrom),
			ToAddr:          firstNonEmpty(parsed.ToAddr, strings.Join(env.Recipients, ","), target.Address),
			Subject:         parsed.Subject,
			TextPreview:     buildPreview(parsed.TextBody),
			HTMLPreview:     parsed.HTMLBody,
			TextBody:        parsed.TextBody,
			HTMLBody:        parsed.HTMLBody,
			Headers:         parsed.Headers,
			RawStorageKey:   rawStorageKey,
			HasAttachments:  len(attachments) > 0,
			SizeBytes:       int64(len(parsed.RawBytes)),
			ReceivedAt:      baseReceivedAt,
			Attachments:     attachments,
		}

		if err := s.store.StoreInbound(ctx, target.ID, item); err != nil {
			return StoredInboundMessage{}, err
		}
		if target.ID != 0 && s.onDelivery != nil {
			s.onDelivery(target.UserID, target.ID, target.Address, item.Subject)
		}
		if s.onForwarding != nil && strings.TrimSpace(target.ForwardTo) != "" {
			s.onForwarding(ctx, target.Address, target.ForwardTo, item.Subject, parsed.RawBytes)
		}
		lastItem = item
		deliveredTo = target
	}

	lastItem.MailboxAddress = deliveredTo.Address
	return lastItem, nil
}

func (s *DirectService) resolveInboundPolicy(ctx context.Context, targets []mailbox.Mailbox) (InboundPolicy, error) {
	if s.inboundPolicyProvider == nil {
		return InboundPolicy{}, nil
	}
	return s.inboundPolicyProvider(ctx, append([]mailbox.Mailbox{}, targets...))
}

func validateInboundMessageAttachments(parsed InboundMessage, policy InboundPolicy) error {
	for _, attachment := range parsed.Attachments {
		if policy.MaxAttachmentSizeBytes > 0 && attachment.SizeBytes > policy.MaxAttachmentSizeBytes {
			name := strings.TrimSpace(attachment.FileName)
			if name == "" {
				name = "attachment"
			}
			return &RejectionError{
				Code:    RejectAttachmentTooLarge,
				Message: fmt.Sprintf("attachment %s exceeds inbound policy size limit", name),
			}
		}
		if policy.RejectExecutableFiles && isExecutableAttachment(attachment) {
			name := strings.TrimSpace(attachment.FileName)
			if name == "" {
				name = "attachment"
			}
			return &RejectionError{
				Code:    RejectExecutableAttachment,
				Message: fmt.Sprintf("attachment %s is blocked by inbound policy", name),
			}
		}
	}
	return nil
}

func mailboxIDsFromTargets(targets []mailbox.Mailbox) []uint64 {
	ids := make([]uint64, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
	}
	return ids
}

func allTargetsHaveMailboxIDs(targets []mailbox.Mailbox) bool {
	for _, target := range targets {
		if target.ID == 0 {
			return false
		}
	}
	return true
}

func buildSourceMessageID(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

func buildPreview(body string) string {
	trimmed := strings.TrimSpace(body)
	if utf8.RuneCountInString(trimmed) <= 160 {
		return trimmed
	}

	count := 0
	for index := range trimmed {
		if count == 160 {
			return trimmed[:index]
		}
		count += 1
	}
	return trimmed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
