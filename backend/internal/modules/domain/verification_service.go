package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"shiro-email/backend/internal/modules/system"
)

func (s *Service) PreviewProviderVerifications(ctx context.Context, providerAccountID uint64, providerZoneID string, zoneName string) ([]VerificationProfile, error) {
	targetKind := s.verificationKindForZoneName(ctx, zoneName, nil)
	return s.previewProviderVerifications(ctx, providerAccountID, providerZoneID, zoneName, targetKind)
}

func (s *Service) previewProviderVerifications(ctx context.Context, providerAccountID uint64, providerZoneID string, zoneName string, targetKind string) ([]VerificationProfile, error) {
	normalizedZoneName := strings.TrimSpace(zoneName)
	if normalizedZoneName == "" {
		return nil, ErrInvalidDNSChangeSetRequest
	}

	records, err := s.ListProviderRecords(ctx, providerAccountID, providerZoneID)
	if err != nil {
		return nil, err
	}

	settings, err := system.LoadMailSMTPSettings(ctx, s.configRepo)
	if err != nil {
		return nil, err
	}

	return buildVerificationProfiles(normalizedZoneName, records, settings, targetKind), nil
}

func buildVerificationProfiles(zoneName string, currentRecords []ProviderRecord, smtpSettings system.MailSMTPConfig, targetKind string) []VerificationProfile {
	now := time.Now()
	mxTarget := strings.TrimSpace(smtpSettings.Hostname)
	if mxTarget == "" {
		mxTarget = "mail.shiro.local"
	}
	dkimTarget := strings.TrimSpace(smtpSettings.DKIMCnameTarget)
	if dkimTarget == "" {
		dkimTarget = "shiro._domainkey.shiro.local"
	}
	profiles := []struct {
		verificationType string
		expectedRecords  []ProviderRecord
		summaryFunc      func(status string, expected []ProviderRecord) string
	}{
		{
			verificationType: "ownership",
			expectedRecords: []ProviderRecord{
				{
					Type:  "TXT",
					Name:  "_shiro-verification." + zoneName,
					Value: "shiro-ownership=" + zoneName,
					TTL:   300,
				},
			},
			summaryFunc: verificationSummary("所有权"),
		},
		{
			verificationType: "inbound_mx",
			expectedRecords: []ProviderRecord{
				{
					Type:     "MX",
					Name:     zoneName,
					Value:    mxTarget,
					TTL:      300,
					Priority: 10,
				},
			},
			summaryFunc: verificationSummary("MX"),
		},
		{
			verificationType: "spf",
			expectedRecords: []ProviderRecord{
				{
					Type:  "TXT",
					Name:  zoneName,
					Value: "v=spf1 mx -all",
					TTL:   300,
				},
			},
			summaryFunc: verificationSummary("SPF"),
		},
		{
			verificationType: "dkim",
			expectedRecords: []ProviderRecord{
				{
					Type:  "CNAME",
					Name:  "shiro._domainkey." + zoneName,
					Value: dkimTarget,
					TTL:   300,
				},
			},
			summaryFunc: verificationSummary("DKIM"),
		},
		{
			verificationType: "dmarc",
			expectedRecords: []ProviderRecord{
				{
					Type:  "TXT",
					Name:  "_dmarc." + zoneName,
					Value: "v=DMARC1; p=quarantine",
					TTL:   300,
				},
			},
			summaryFunc: verificationSummary("DMARC"),
		},
	}
	if strings.EqualFold(strings.TrimSpace(targetKind), "subdomain") {
		profiles = profiles[1:2]
	}

	items := make([]VerificationProfile, 0, len(profiles))
	for _, profile := range profiles {
		expectedRecords := make([]ProviderRecord, len(profile.expectedRecords))
		copy(expectedRecords, profile.expectedRecords)
		observedRecords, status := collectVerificationObservedRecords(expectedRecords, currentRecords)
		repairRecords := []ProviderRecord{}
		if status != "verified" {
			repairRecords = expectedRecords
		}
		items = append(items, VerificationProfile{
			VerificationType: profile.verificationType,
			Status:           status,
			Summary:          profile.summaryFunc(status, expectedRecords),
			ExpectedRecords:  expectedRecords,
			ObservedRecords:  observedRecords,
			RepairRecords:    repairRecords,
			LastCheckedAt:    now,
		})
	}

	return items
}

func (s *Service) verificationKindForZoneName(ctx context.Context, zoneName string, ownerUserID *uint64) string {
	if s == nil || s.repo == nil {
		return ""
	}
	item, err := s.repo.FindByDomain(ctx, strings.TrimSpace(zoneName))
	if err != nil {
		return ""
	}
	if ownerUserID != nil {
		if item.OwnerUserID == nil || *item.OwnerUserID != *ownerUserID {
			return ""
		}
	}
	return item.Kind
}

func collectVerificationObservedRecords(expectedRecords []ProviderRecord, currentRecords []ProviderRecord) ([]ProviderRecord, string) {
	observedRecords := make([]ProviderRecord, 0)
	matchedCount := 0
	driftedCount := 0

	for _, expectedRecord := range expectedRecords {
		candidates := findVerificationCandidates(currentRecords, expectedRecord)
		observedRecords = append(observedRecords, candidates...)
		if len(candidates) == 0 {
			continue
		}
		exactMatch := false
		for _, candidate := range candidates {
			if verificationRecordsEqual(candidate, expectedRecord) {
				exactMatch = true
				break
			}
		}
		if exactMatch {
			matchedCount++
			continue
		}
		driftedCount++
	}

	switch {
	case matchedCount == len(expectedRecords):
		return observedRecords, "verified"
	case matchedCount > 0 || driftedCount > 0:
		return observedRecords, "drifted"
	default:
		return observedRecords, "missing"
	}
}

func findVerificationCandidates(currentRecords []ProviderRecord, expectedRecord ProviderRecord) []ProviderRecord {
	items := make([]ProviderRecord, 0)
	expectedIdentity := verificationRecordIdentity(expectedRecord)
	for _, currentRecord := range currentRecords {
		if verificationRecordIdentity(currentRecord) != expectedIdentity {
			continue
		}
		items = append(items, currentRecord)
	}
	return items
}

func verificationRecordIdentity(record ProviderRecord) string {
	normalized := normalizeProviderRecord(record)
	return strings.Join([]string{
		normalized.Type,
		strings.ToLower(strings.TrimSuffix(normalized.Name, ".")),
	}, "|")
}

func verificationRecordsEqual(left ProviderRecord, right ProviderRecord) bool {
	left = normalizeProviderRecord(left)
	right = normalizeProviderRecord(right)
	if verificationRecordIdentity(left) != verificationRecordIdentity(right) {
		return false
	}
	return normalizeVerificationRecordValue(left) == normalizeVerificationRecordValue(right)
}

func normalizeVerificationRecordValue(record ProviderRecord) string {
	value := strings.TrimSpace(record.Value)
	switch strings.ToUpper(strings.TrimSpace(record.Type)) {
	case "CNAME", "MX", "NS", "PTR":
		return strings.ToLower(strings.TrimSuffix(value, "."))
	default:
		return value
	}
}

func verificationSummary(label string) func(status string, expected []ProviderRecord) string {
	return func(status string, expected []ProviderRecord) string {
		switch status {
		case "verified":
			return fmt.Sprintf("%s 配置已就绪", label)
		case "drifted":
			return fmt.Sprintf("%s 记录存在漂移，建议修复 %d 条记录", label, len(expected))
		default:
			return fmt.Sprintf("%s 记录缺失，建议补齐 %d 条记录", label, len(expected))
		}
	}
}
