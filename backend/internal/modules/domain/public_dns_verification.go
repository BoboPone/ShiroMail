package domain

import (
	"context"
	"net"
	"strings"
	"time"

	"shiro-email/backend/internal/modules/system"
)

type publicDNSResolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
}

func defaultPublicDNSResolver() publicDNSResolver {
	return publicResolverSet{
		resolvers: []*net.Resolver{
			newPublicResolver("223.5.5.5:53"),
			newPublicResolver("119.29.29.29:53"),
			newPublicResolver("8.8.8.8:53"),
			newPublicResolver("1.1.1.1:53"),
			net.DefaultResolver,
		},
	}
}

type publicResolverSet struct {
	resolvers []*net.Resolver
}

func (r publicResolverSet) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	var lastErr error
	for _, resolver := range r.resolvers {
		items, err := resolver.LookupMX(ctx, name)
		if err == nil && len(items) > 0 {
			return items, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	return nil, lastErr
}

func (r publicResolverSet) LookupTXT(ctx context.Context, name string) ([]string, error) {
	var lastErr error
	for _, resolver := range r.resolvers {
		items, err := resolver.LookupTXT(ctx, name)
		if err == nil && len(items) > 0 {
			return items, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	return nil, lastErr
}

func (r publicResolverSet) LookupCNAME(ctx context.Context, host string) (string, error) {
	var lastErr error
	for _, resolver := range r.resolvers {
		value, err := resolver.LookupCNAME(ctx, host)
		if err == nil && strings.TrimSpace(value) != "" {
			return value, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	return "", lastErr
}

func newPublicResolver(server string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network string, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 5 * time.Second}
			return dialer.DialContext(ctx, network, server)
		},
	}
}

func (s *Service) PreviewPublicDNSVerifications(ctx context.Context, target Domain) ([]VerificationProfile, error) {
	normalizedZoneName := strings.TrimSpace(target.Domain)
	if normalizedZoneName == "" {
		return nil, ErrInvalidDNSChangeSetRequest
	}

	settings, err := system.LoadMailSMTPSettings(ctx, s.configRepo)
	if err != nil {
		return nil, err
	}

	expectedProfiles := publicDNSVerificationProfiles(buildVerificationProfiles(normalizedZoneName, nil, settings, target.Kind))
	records := s.lookupPublicDNSRecords(ctx, expectedProfiles)
	return publicDNSVerificationProfiles(buildVerificationProfiles(normalizedZoneName, records, settings, target.Kind)), nil
}

func publicDNSVerificationProfiles(profiles []VerificationProfile) []VerificationProfile {
	filtered := make([]VerificationProfile, 0, len(profiles))
	for _, profile := range profiles {
		switch profile.VerificationType {
		case "ownership", "inbound_mx":
			filtered = append(filtered, profile)
		}
	}
	return filtered
}

func (s *Service) lookupPublicDNSRecords(ctx context.Context, profiles []VerificationProfile) []ProviderRecord {
	resolver := s.publicDNS
	if resolver == nil {
		resolver = defaultPublicDNSResolver()
	}

	seen := map[string]struct{}{}
	records := make([]ProviderRecord, 0)
	for _, profile := range profiles {
		for _, expected := range profile.ExpectedRecords {
			record := normalizeProviderRecord(expected)
			key := record.Type + "|" + strings.ToLower(record.Name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			records = append(records, lookupPublicDNSRecordSet(ctx, resolver, record)...)
		}
	}
	return records
}

func lookupPublicDNSRecordSet(ctx context.Context, resolver publicDNSResolver, expected ProviderRecord) []ProviderRecord {
	switch strings.ToUpper(strings.TrimSpace(expected.Type)) {
	case "MX":
		mxRecords, err := resolver.LookupMX(ctx, expected.Name)
		if err != nil {
			return nil
		}
		items := make([]ProviderRecord, 0, len(mxRecords))
		for _, mx := range mxRecords {
			items = append(items, ProviderRecord{
				Type:     "MX",
				Name:     expected.Name,
				Value:    trimDNSDot(mx.Host),
				TTL:      expected.TTL,
				Priority: int(mx.Pref),
			})
		}
		return items
	case "TXT":
		txtRecords, err := resolver.LookupTXT(ctx, expected.Name)
		if err != nil {
			return nil
		}
		items := make([]ProviderRecord, 0, len(txtRecords))
		for _, value := range txtRecords {
			items = append(items, ProviderRecord{
				Type:  "TXT",
				Name:  expected.Name,
				Value: strings.TrimSpace(value),
				TTL:   expected.TTL,
			})
		}
		return items
	case "CNAME":
		value, err := resolver.LookupCNAME(ctx, expected.Name)
		if err != nil {
			return nil
		}
		return []ProviderRecord{{
			Type:  "CNAME",
			Name:  expected.Name,
			Value: trimDNSDot(value),
			TTL:   expected.TTL,
		}}
	default:
		return nil
	}
}

func trimDNSDot(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), ".")
}
