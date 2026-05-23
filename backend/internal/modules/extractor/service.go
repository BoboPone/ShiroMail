package extractor

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"shiro-email/backend/internal/modules/mailbox"
	"shiro-email/backend/internal/modules/message"
	"shiro-email/backend/internal/modules/system"
)

type Service struct {
	repo           Repository
	mailboxRepo    mailbox.Repository
	messageService *message.Service
	auditRepo      system.AuditRepository
}

func NewService(repo Repository, mailboxRepo mailbox.Repository, messageService *message.Service, auditRepo system.AuditRepository) *Service {
	return &Service{
		repo:           repo,
		mailboxRepo:    mailboxRepo,
		messageService: messageService,
		auditRepo:      auditRepo,
	}
}

func (s *Service) ListPortalRules(ctx context.Context, userID uint64) (RuleList, error) {
	userRules, err := s.repo.ListUserRules(ctx, userID)
	if err != nil {
		return RuleList{}, err
	}
	userRules, err = s.sanitizeRules(ctx, userRules, RuleSourceUser, &userID)
	if err != nil {
		return RuleList{}, err
	}
	templates, err := s.repo.ListAdminTemplates(ctx)
	if err != nil {
		return RuleList{}, err
	}
	templates, err = s.sanitizeRules(ctx, templates, RuleSourceAdminDefault, nil)
	if err != nil {
		return RuleList{}, err
	}
	enabledTemplateIDs, err := s.repo.ListEnabledTemplateIDs(ctx, userID)
	if err != nil {
		return RuleList{}, err
	}
	return RuleList{
		Rules:     toRuleViews(userRules, nil),
		Templates: toRuleViews(templates, enabledTemplateIDs),
	}, nil
}

func (s *Service) ListAdminRules(ctx context.Context) ([]Rule, error) {
	items, err := s.repo.ListAdminTemplates(ctx)
	if err != nil {
		return nil, err
	}
	return s.sanitizeRules(ctx, items, RuleSourceAdminDefault, nil)
}

func (s *Service) CreatePortalRule(ctx context.Context, userID uint64, input UpsertRuleInput) (Rule, error) {
	return s.createRule(ctx, RuleSourceUser, &userID, input)
}

func (s *Service) UpdatePortalRule(ctx context.Context, userID uint64, ruleID uint64, input UpsertRuleInput) (Rule, error) {
	item, err := s.repo.FindRuleByID(ctx, ruleID)
	if err != nil {
		return Rule{}, err
	}
	if item.OwnerUserID == nil || *item.OwnerUserID != userID || item.SourceType != RuleSourceUser {
		return Rule{}, ErrRuleNotFound
	}
	normalized, err := normalizeRule(input, RuleSourceUser, &userID)
	if err != nil {
		return Rule{}, err
	}
	normalized, err = s.sanitizeRuleScopes(ctx, normalized, RuleSourceUser, &userID)
	if err != nil {
		return Rule{}, err
	}
	normalized.ID = item.ID
	normalized.CreatedAt = item.CreatedAt
	normalized.TemplateKey = item.TemplateKey
	return s.repo.UpdateRule(ctx, normalized)
}

func (s *Service) DeletePortalRule(ctx context.Context, userID uint64, ruleID uint64) error {
	item, err := s.repo.FindRuleByID(ctx, ruleID)
	if err != nil {
		return err
	}
	if item.OwnerUserID == nil || *item.OwnerUserID != userID || item.SourceType != RuleSourceUser {
		return ErrRuleNotFound
	}
	return s.repo.DeleteRule(ctx, ruleID)
}

func (s *Service) CreateAdminRule(ctx context.Context, actorID uint64, input UpsertRuleInput) (Rule, error) {
	rule, err := s.createRule(ctx, RuleSourceAdminDefault, nil, input)
	if err != nil {
		return Rule{}, err
	}
	s.audit("admin.extractor.create", actorID, rule)
	return rule, nil
}

func (s *Service) UpdateAdminRule(ctx context.Context, actorID uint64, ruleID uint64, input UpsertRuleInput) (Rule, error) {
	item, err := s.repo.FindRuleByID(ctx, ruleID)
	if err != nil {
		return Rule{}, err
	}
	if item.SourceType != RuleSourceAdminDefault {
		return Rule{}, ErrRuleNotFound
	}
	normalized, err := normalizeRule(input, RuleSourceAdminDefault, nil)
	if err != nil {
		return Rule{}, err
	}
	normalized, err = s.sanitizeRuleScopes(ctx, normalized, RuleSourceAdminDefault, nil)
	if err != nil {
		return Rule{}, err
	}
	normalized.ID = item.ID
	normalized.TemplateKey = item.TemplateKey
	normalized.CreatedAt = item.CreatedAt
	updated, err := s.repo.UpdateRule(ctx, normalized)
	if err != nil {
		return Rule{}, err
	}
	s.audit("admin.extractor.update", actorID, updated)
	return updated, nil
}

func (s *Service) DeleteAdminRule(ctx context.Context, actorID uint64, ruleID uint64) error {
	item, err := s.repo.FindRuleByID(ctx, ruleID)
	if err != nil {
		return err
	}
	if item.SourceType != RuleSourceAdminDefault {
		return ErrRuleNotFound
	}
	if err := s.repo.DeleteRule(ctx, ruleID); err != nil {
		return err
	}
	s.audit("admin.extractor.delete", actorID, item)
	return nil
}

func (s *Service) EnableTemplate(ctx context.Context, userID uint64, ruleID uint64) error {
	item, err := s.repo.FindRuleByID(ctx, ruleID)
	if err != nil {
		return err
	}
	if item.SourceType != RuleSourceAdminDefault {
		return ErrRuleNotFound
	}
	return s.repo.SetTemplateEnabled(ctx, userID, ruleID, true)
}

func (s *Service) DisableTemplate(ctx context.Context, userID uint64, ruleID uint64) error {
	item, err := s.repo.FindRuleByID(ctx, ruleID)
	if err != nil {
		return err
	}
	if item.SourceType != RuleSourceAdminDefault {
		return ErrRuleNotFound
	}
	return s.repo.SetTemplateEnabled(ctx, userID, ruleID, false)
}

func (s *Service) CopyTemplateToUser(ctx context.Context, userID uint64, ruleID uint64) (Rule, error) {
	item, err := s.repo.FindRuleByID(ctx, ruleID)
	if err != nil {
		return Rule{}, err
	}
	if item.SourceType != RuleSourceAdminDefault {
		return Rule{}, ErrRuleNotFound
	}
	input := UpsertRuleInput{
		Name:              item.Name,
		Description:       item.Description,
		Label:             item.Label,
		Enabled:           item.Enabled,
		TargetFields:      item.TargetFields,
		Pattern:           item.Pattern,
		Flags:             item.Flags,
		ResultMode:        item.ResultMode,
		CaptureGroupIndex: item.CaptureGroupIndex,
		MailboxIDs:        item.MailboxIDs,
		DomainIDs:         item.DomainIDs,
		SenderContains:    item.SenderContains,
		SubjectContains:   item.SubjectContains,
		SortOrder:         item.SortOrder,
	}
	return s.CreatePortalRule(ctx, userID, input)
}

func (s *Service) TestPortalRule(ctx context.Context, userID uint64, input UpsertRuleInput, sample RuleTestInput) (RuleTestResult, error) {
	rule, err := normalizeRule(input, RuleSourceUser, &userID)
	if err != nil {
		return RuleTestResult{}, err
	}
	rule, err = s.sanitizeRuleScopes(ctx, rule, RuleSourceUser, &userID)
	if err != nil {
		return RuleTestResult{}, err
	}
	content, err := s.resolveTestContent(ctx, userID, sample, false)
	if err != nil {
		return RuleTestResult{}, err
	}
	items, err := extractMatches(rule, content)
	if err != nil {
		return RuleTestResult{}, err
	}
	return RuleTestResult{Items: items}, nil
}

func (s *Service) TestAdminRule(ctx context.Context, input UpsertRuleInput, sample RuleTestInput) (RuleTestResult, error) {
	rule, err := normalizeRule(input, RuleSourceAdminDefault, nil)
	if err != nil {
		return RuleTestResult{}, err
	}
	rule, err = s.sanitizeRuleScopes(ctx, rule, RuleSourceAdminDefault, nil)
	if err != nil {
		return RuleTestResult{}, err
	}
	content, err := s.resolveTestContent(ctx, 0, sample, true)
	if err != nil {
		return RuleTestResult{}, err
	}
	items, err := extractMatches(rule, content)
	if err != nil {
		return RuleTestResult{}, err
	}
	return RuleTestResult{Items: items}, nil
}

func (s *Service) ExtractForPortalMessage(ctx context.Context, userID uint64, mailboxID uint64, messageID uint64) (ExtractionResult, error) {
	content, err := s.loadMessageContent(ctx, userID, mailboxID, messageID, false)
	if err != nil {
		return ExtractionResult{}, err
	}
	rules, err := s.effectiveRulesForUser(ctx, userID)
	if err != nil {
		return ExtractionResult{}, err
	}
	return extractForRules(rules, content)
}

func (s *Service) ExtractForAdminMessage(ctx context.Context, mailboxID uint64, messageID uint64) (ExtractionResult, error) {
	content, err := s.loadMessageContent(ctx, 0, mailboxID, messageID, true)
	if err != nil {
		return ExtractionResult{}, err
	}
	rules, err := s.repo.ListAdminTemplates(ctx)
	if err != nil {
		return ExtractionResult{}, err
	}
	return extractForRules(enabledRulesOnly(rules), content)
}

func (s *Service) createRule(ctx context.Context, sourceType RuleSourceType, ownerUserID *uint64, input UpsertRuleInput) (Rule, error) {
	rule, err := normalizeRule(input, sourceType, ownerUserID)
	if err != nil {
		return Rule{}, err
	}
	rule, err = s.sanitizeRuleScopes(ctx, rule, sourceType, ownerUserID)
	if err != nil {
		return Rule{}, err
	}
	if sourceType == RuleSourceAdminDefault && strings.TrimSpace(rule.TemplateKey) == "" {
		rule.TemplateKey = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(rule.Name), " ", "-"))
	}
	return s.repo.CreateRule(ctx, rule)
}

func (s *Service) effectiveRulesForUser(ctx context.Context, userID uint64) ([]Rule, error) {
	userRules, err := s.repo.ListUserRules(ctx, userID)
	if err != nil {
		return nil, err
	}
	templates, err := s.repo.ListAdminTemplates(ctx)
	if err != nil {
		return nil, err
	}
	templates, err = s.sanitizeRules(ctx, templates, RuleSourceAdminDefault, nil)
	if err != nil {
		return nil, err
	}
	enabledTemplateIDs, err := s.repo.ListEnabledTemplateIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	userRules, err = s.sanitizeRules(ctx, userRules, RuleSourceUser, &userID)
	if err != nil {
		return nil, err
	}
	items := enabledRulesOnly(userRules)
	for _, template := range templates {
		if !template.Enabled || !slices.Contains(enabledTemplateIDs, template.ID) {
			continue
		}
		items = append(items, template)
	}
	slices.SortFunc(items, func(a Rule, b Rule) int {
		if a.SortOrder != b.SortOrder {
			return a.SortOrder - b.SortOrder
		}
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
	return items, nil
}

func (s *Service) loadMessageContent(ctx context.Context, userID uint64, mailboxID uint64, messageID uint64, admin bool) (MessageContent, error) {
	var (
		msg      message.Message
		err      error
		parsed   message.ParsedRawMessage
		rawText  string
		rawEmail message.Download
	)
	if s.messageService == nil {
		return MessageContent{}, errors.New("message service unavailable")
	}
	if admin {
		msg, err = s.messageService.GetByMailboxAndIDForAdmin(ctx, mailboxID, messageID)
	} else {
		msg, err = s.messageService.GetByMailboxAndID(ctx, userID, mailboxID, messageID)
	}
	if err != nil {
		return MessageContent{}, err
	}
	box, err := s.mailboxRepo.FindByID(ctx, mailboxID)
	if err != nil {
		return MessageContent{}, err
	}

	if admin {
		rawEmail, err = s.messageService.DownloadRawByMailboxAndIDForAdmin(ctx, mailboxID, messageID)
	} else {
		rawEmail, err = s.messageService.DownloadRawByMailboxAndID(ctx, userID, mailboxID, messageID)
	}
	if err == nil {
		rawText = string(rawEmail.Content)
	}

	if admin {
		parsed, err = s.messageService.ParseRawByMailboxAndIDForAdmin(ctx, mailboxID, messageID)
	} else {
		parsed, err = s.messageService.ParseRawByMailboxAndID(ctx, userID, mailboxID, messageID)
	}
	if err == nil && strings.TrimSpace(rawText) == "" {
		rawText = strings.TrimSpace(parsed.TextBody)
	}

	textBody := msg.TextBody
	if strings.TrimSpace(textBody) == "" {
		textBody = rawText
	}

	return MessageContent{
		MailboxID: mailboxID,
		DomainID:  box.DomainID,
		Subject:   msg.Subject,
		FromAddr:  msg.FromAddr,
		ToAddr:    msg.ToAddr,
		TextBody:  textBody,
		HTMLBody:  msg.HTMLBody,
		RawText:   rawText,
	}, nil
}

func (s *Service) resolveTestContent(ctx context.Context, userID uint64, sample RuleTestInput, admin bool) (MessageContent, error) {
	if sample.MailboxID != nil && sample.MessageID != nil {
		return s.loadMessageContent(ctx, userID, *sample.MailboxID, *sample.MessageID, admin)
	}
	return MessageContent{
		Subject:  sample.Subject,
		FromAddr: sample.FromAddr,
		ToAddr:   sample.ToAddr,
		TextBody: sample.TextBody,
		HTMLBody: sample.HTMLBody,
		RawText:  sample.RawText,
	}, nil
}

func extractForRules(rules []Rule, content MessageContent) (ExtractionResult, error) {
	items := make([]ExtractionMatch, 0)
	for _, rule := range rules {
		matches, err := extractMatches(rule, content)
		if err != nil {
			return ExtractionResult{}, err
		}
		items = append(items, matches...)
	}
	return ExtractionResult{Items: items}, nil
}

func enabledRulesOnly(items []Rule) []Rule {
	filtered := make([]Rule, 0, len(items))
	for _, item := range items {
		if item.Enabled {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func toRuleViews(items []Rule, enabledTemplateIDs []uint64) []RuleView {
	result := make([]RuleView, 0, len(items))
	for _, item := range items {
		result = append(result, RuleView{
			Rule:           item,
			EnabledForUser: slices.Contains(enabledTemplateIDs, item.ID),
		})
	}
	return result
}

func (s *Service) audit(action string, actorID uint64, rule Rule) {
	if s.auditRepo == nil || actorID == 0 {
		return
	}
	_, _ = s.auditRepo.Create(context.Background(), actorID, action, "mail_extractor_rule", strings.TrimSpace(rule.Name), map[string]any{
		"id":   rule.ID,
		"name": rule.Name,
	})
}

func (s *Service) sanitizeRules(ctx context.Context, items []Rule, sourceType RuleSourceType, ownerUserID *uint64) ([]Rule, error) {
	sanitized := make([]Rule, 0, len(items))
	for _, item := range items {
		next, err := s.sanitizeRuleScopes(ctx, item, sourceType, ownerUserID)
		if err != nil {
			return nil, err
		}
		sanitized = append(sanitized, next)
	}
	return sanitized, nil
}

func (s *Service) sanitizeRuleScopes(ctx context.Context, rule Rule, sourceType RuleSourceType, ownerUserID *uint64) (Rule, error) {
	rule.MailboxIDs = uniqueUint64s(rule.MailboxIDs)
	if len(rule.MailboxIDs) == 0 || s.mailboxRepo == nil {
		return rule, nil
	}

	now := time.Now()
	filtered := make([]uint64, 0, len(rule.MailboxIDs))
	for _, mailboxID := range rule.MailboxIDs {
		var (
			item mailbox.Mailbox
			err  error
		)
		switch {
		case sourceType == RuleSourceUser && ownerUserID != nil:
			item, err = s.mailboxRepo.FindByUserAndID(ctx, *ownerUserID, mailboxID)
		default:
			item, err = s.mailboxRepo.FindByID(ctx, mailboxID)
		}
		if err != nil {
			continue
		}
		if !mailbox.IsActiveAt(item, now) {
			continue
		}
		filtered = append(filtered, mailboxID)
	}
	rule.MailboxIDs = filtered
	return rule, nil
}
