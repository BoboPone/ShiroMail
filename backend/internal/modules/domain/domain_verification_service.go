package domain

import (
	"context"
	"strings"

	"shiro-email/backend/internal/modules/portal"
)

type DomainVerificationResult struct {
	Domain        Domain                `json:"domain"`
	Passed        bool                  `json:"passed"`
	Summary       string                `json:"summary"`
	ZoneName      string                `json:"zoneName,omitempty"`
	Profiles      []VerificationProfile `json:"profiles"`
	VerifiedCount int                   `json:"verifiedCount"`
	TotalCount    int                   `json:"totalCount"`
}

func summarizeDomainVerification(profiles []VerificationProfile) (passed bool, verifiedCount int) {
	for _, profile := range profiles {
		if profile.Status == "verified" {
			verifiedCount++
		}
	}
	return verifiedCount == len(profiles) && len(profiles) > 0, verifiedCount
}

func (s *Service) VerifyOwnedDomain(ctx context.Context, userID uint64, domainID uint64, apiKeys ...portal.APIKey) (DomainVerificationResult, error) {
	target, err := s.repo.GetActiveByID(ctx, domainID)
	if err != nil {
		return DomainVerificationResult{}, err
	}
	if target.OwnerUserID == nil || *target.OwnerUserID != userID {
		return DomainVerificationResult{}, ErrDomainNotFound
	}
	if apiKey := optionalAPIKey(apiKeys...); apiKey != nil && !portal.APIKeyAllowsBoundResource(*apiKey, userID, boundResourceFromDomain(target), "verify") {
		return DomainVerificationResult{}, portal.ErrAPIKeyForbidden
	}
	return s.verifyDomain(ctx, target)
}

func (s *Service) VerifyDomain(ctx context.Context, domainID uint64) (DomainVerificationResult, error) {
	target, err := s.repo.GetActiveByID(ctx, domainID)
	if err != nil {
		return DomainVerificationResult{}, err
	}
	return s.verifyDomain(ctx, target)
}

func (s *Service) verifyDomain(ctx context.Context, target Domain) (DomainVerificationResult, error) {
	if target.ProviderAccountID == nil {
		profiles, err := s.PreviewPublicDNSVerifications(ctx, target)
		if err != nil {
			return DomainVerificationResult{}, err
		}
		passed, verifiedCount := summarizeDomainVerification(profiles)
		target.VerificationScore = verifiedCount * 100 / max(len(profiles), 1)
		if passed {
			target.HealthStatus = "healthy"
			target.VerificationScore = 100
		} else if verifiedCount > 0 {
			target.HealthStatus = "degraded"
		} else {
			target.HealthStatus = "unknown"
		}
		updated, err := s.repo.Upsert(ctx, target)
		if err != nil {
			return DomainVerificationResult{}, err
		}
		s.invalidateDomainCaches(ctx)
		summary := "未绑定 DNS Provider，已通过公网 DNS 查询验证；仍有记录未通过，请检查建议记录和 DNS 传播。"
		if passed {
			summary = "未绑定 DNS Provider，已通过公网 DNS 查询验证，域名已标记为已验证。"
		}
		return DomainVerificationResult{
			Domain:        updated,
			Passed:        passed,
			Summary:       summary,
			ZoneName:      target.Domain,
			Profiles:      profiles,
			VerifiedCount: verifiedCount,
			TotalCount:    len(profiles),
		}, nil
	}

	zones, err := s.ListProviderZones(ctx, *target.ProviderAccountID)
	if err != nil {
		return DomainVerificationResult{}, err
	}

	var matchedZone *ProviderZone
	for _, zone := range zones {
		if strings.EqualFold(zone.Name, target.RootDomain) || strings.EqualFold(zone.Name, target.Domain) {
			zoneCopy := zone
			matchedZone = &zoneCopy
			break
		}
	}

	if matchedZone == nil {
		target.HealthStatus = "unknown"
		target.VerificationScore = 0
		updated, err := s.repo.Upsert(ctx, target)
		if err != nil {
			return DomainVerificationResult{}, err
		}
		s.invalidateDomainCaches(ctx)
		return DomainVerificationResult{
			Domain:   updated,
			Passed:   false,
			Summary:  "已连接 DNS 服务商，但没有匹配到该域名对应的 Zone。",
			Profiles: []VerificationProfile{},
		}, nil
	}

	profiles, err := s.previewProviderVerifications(ctx, *target.ProviderAccountID, matchedZone.ID, target.Domain, target.Kind)
	if err != nil {
		return DomainVerificationResult{}, err
	}

	passed, verifiedCount := summarizeDomainVerification(profiles)
	target.VerificationScore = verifiedCount * 100 / max(len(profiles), 1)
	if passed {
		target.HealthStatus = "healthy"
		target.VerificationScore = 100
	} else if verifiedCount > 0 {
		target.HealthStatus = "degraded"
	} else {
		target.HealthStatus = "unknown"
	}

	updated, err := s.repo.Upsert(ctx, target)
	if err != nil {
		return DomainVerificationResult{}, err
	}
	s.invalidateDomainCaches(ctx)

	summary := "DNS 传播验证未通过，请根据缺失或漂移记录继续修复。"
	if passed {
		summary = "DNS 传播验证通过，域名已标记为已验证。"
	}

	return DomainVerificationResult{
		Domain:        updated,
		Passed:        passed,
		Summary:       summary,
		ZoneName:      matchedZone.Name,
		Profiles:      profiles,
		VerifiedCount: verifiedCount,
		TotalCount:    len(profiles),
	}, nil
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
