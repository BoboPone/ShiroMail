package domain

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	domainprovider "shiro-email/backend/internal/modules/domain/provider"
	"shiro-email/backend/internal/modules/portal"
	"shiro-email/backend/internal/modules/system"
	sharedcache "shiro-email/backend/internal/shared/cache"
)

var ErrInvalidDomain = errors.New("invalid domain")
var ErrDomainAlreadyExists = errors.New("domain already exists")
var ErrInvalidPublicationState = errors.New("invalid publication state")
var ErrProviderAdapterUnavailable = errors.New("provider adapter unavailable")
var ErrDomainHasChildren = errors.New("domain has child domains")
var ErrDomainHasMailboxes = errors.New("domain has mailboxes")
var ErrProviderAccountInUse = errors.New("provider account in use")
var ErrProviderAccountImmutableFieldsLocked = errors.New("provider and auth type cannot be changed while domains are bound")

type CreateDomainRequest struct {
	Domain            string  `json:"domain" binding:"required"`
	Status            string  `json:"status"`
	Visibility        string  `json:"visibility"`
	PublicationStatus string  `json:"publicationStatus"`
	VerificationScore int     `json:"verificationScore"`
	HealthStatus      string  `json:"healthStatus"`
	ProviderAccountID *uint64 `json:"providerAccountId"`
	IsDefault         bool    `json:"isDefault"`
	Weight            int     `json:"weight"`
}

type GenerateSubdomainsRequest struct {
	BaseDomainID      uint64   `json:"baseDomainId" binding:"required"`
	Prefixes          []string `json:"prefixes" binding:"required"`
	Status            string   `json:"status"`
	Visibility        string   `json:"visibility"`
	PublicationStatus string   `json:"publicationStatus"`
	VerificationScore int      `json:"verificationScore"`
	HealthStatus      string   `json:"healthStatus"`
	ProviderAccountID *uint64  `json:"providerAccountId"`
	Weight            int      `json:"weight"`
}

type UpdateOwnedDomainProviderBindingRequest struct {
	ProviderAccountID *uint64 `json:"providerAccountId"`
}

type CreateProviderAccountRequest struct {
	Provider     string                   `json:"provider" binding:"required"`
	OwnerType    string                   `json:"ownerType"`
	OwnerUserID  *uint64                  `json:"ownerUserId"`
	DisplayName  string                   `json:"displayName" binding:"required"`
	AuthType     string                   `json:"authType"`
	SecretRef    string                   `json:"secretRef"`
	Credentials  ProviderCredentialsInput `json:"credentials"`
	Status       string                   `json:"status"`
	Capabilities []string                 `json:"capabilities"`
}

type ProviderCredentialsInput struct {
	APIToken  string `json:"apiToken"`
	APIEmail  string `json:"apiEmail"`
	APIKey    string `json:"apiKey"`
	APISecret string `json:"apiSecret"`
}

type DomainMailboxUsageChecker func(ctx context.Context, domainID uint64) (bool, error)
type DomainMailboxCleanupFunc func(ctx context.Context, domainID uint64) error

type Service struct {
	repo             Repository
	hasMailboxes     DomainMailboxUsageChecker
	cleanupMailboxes DomainMailboxCleanupFunc
	configRepo       system.ConfigRepository
	auditRepo        system.AuditRepository
	cache            *sharedcache.JSONCache
	providers        *domainprovider.Registry
	publicDNS        publicDNSResolver
}

func NewService(repo Repository, hasMailboxes DomainMailboxUsageChecker, cleanupMailboxes DomainMailboxCleanupFunc, configRepo system.ConfigRepository, auditRepo system.AuditRepository, providers *domainprovider.Registry, cache ...*sharedcache.JSONCache) *Service {
	return &Service{
		repo:             repo,
		hasMailboxes:     hasMailboxes,
		cleanupMailboxes: cleanupMailboxes,
		configRepo:       configRepo,
		auditRepo:        auditRepo,
		cache:            optionalJSONCache(cache),
		providers:        providers,
		publicDNS:        defaultPublicDNSResolver(),
	}
}

func (s *Service) ListActive(ctx context.Context) ([]Domain, error) {
	return s.repo.ListActive(ctx)
}

func (s *Service) ListAccessibleActive(ctx context.Context, userID uint64, apiKeys ...portal.APIKey) ([]Domain, error) {
	apiKey := optionalAPIKey(apiKeys...)
	if apiKey == nil {
		items, err := s.repo.ListAccessibleActive(ctx, userID)
		if err != nil {
			return nil, err
		}
		return items, nil
	}

	items, err := s.repo.ListActive(ctx)
	if err != nil {
		return nil, err
	}

	filtered := make([]Domain, 0, len(items))
	for _, item := range items {
		resource := boundResourceFromDomain(item)
		if portal.APIKeyAllowsBoundResource(*apiKey, userID, resource, "read") ||
			portal.APIKeyAllowsExplicitPrivateBinding(*apiKey, resource, "read") {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *Service) ListAll(ctx context.Context) ([]Domain, error) {
	return s.repo.ListAll(ctx)
}

func (s *Service) ListProviderAccounts(ctx context.Context) ([]ProviderAccount, error) {
	return s.repo.ListProviderAccounts(ctx)
}

func (s *Service) ListOwnedProviderAccounts(ctx context.Context, userID uint64) ([]ProviderAccount, error) {
	items, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return nil, err
	}

	owned := make([]ProviderAccount, 0, len(items))
	for _, item := range items {
		if item.OwnerUserID != nil && *item.OwnerUserID == userID {
			owned = append(owned, item)
		}
	}
	return owned, nil
}

func (s *Service) CreateProviderAccount(ctx context.Context, req CreateProviderAccountRequest) (ProviderAccount, error) {
	secretRef := strings.TrimSpace(req.SecretRef)
	if secretRef == "" {
		secretRef = buildProviderSecretRef(
			strings.TrimSpace(req.Provider),
			defaultAuthType(req.AuthType),
			req.Credentials,
		)
	}

	return s.repo.CreateProviderAccount(ctx, ProviderAccount{
		Provider:     strings.TrimSpace(req.Provider),
		OwnerType:    defaultOwnerType(req.OwnerType),
		OwnerUserID:  req.OwnerUserID,
		DisplayName:  strings.TrimSpace(req.DisplayName),
		AuthType:     defaultAuthType(req.AuthType),
		SecretRef:    secretRef,
		Status:       defaultProviderStatus(req.Status),
		Capabilities: sanitizeCapabilities(req.Capabilities),
	})
}

func (s *Service) CreateOwnedProviderAccount(ctx context.Context, userID uint64, req CreateProviderAccountRequest) (ProviderAccount, error) {
	req.OwnerType = "user"
	req.OwnerUserID = &userID
	return s.CreateProviderAccount(ctx, req)
}

func (s *Service) UpdateProviderAccount(ctx context.Context, providerAccountID uint64, req CreateProviderAccountRequest) (ProviderAccount, error) {
	existing, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return ProviderAccount{}, err
	}

	secretRef := strings.TrimSpace(req.SecretRef)
	if secretRef == "" && hasProviderCredentials(req.Credentials) {
		secretRef = buildProviderSecretRef(
			strings.TrimSpace(req.Provider),
			defaultAuthType(req.AuthType),
			req.Credentials,
		)
	}
	if secretRef == "" {
		secretRef = existing.SecretRef
	}

	existing.Provider = strings.TrimSpace(req.Provider)
	existing.OwnerType = defaultOwnerType(req.OwnerType)
	existing.OwnerUserID = req.OwnerUserID
	existing.DisplayName = strings.TrimSpace(req.DisplayName)
	existing.AuthType = defaultAuthType(req.AuthType)
	existing.SecretRef = secretRef
	existing.Status = defaultProviderStatus(req.Status)
	existing.Capabilities = sanitizeCapabilities(req.Capabilities)

	return s.repo.UpdateProviderAccount(ctx, existing)
}

func buildProviderSecretRef(provider string, authType string, credentials ProviderCredentialsInput) string {
	var payload map[string]string

	switch provider {
	case "spaceship":
		payload = map[string]string{
			"apiKey":    strings.TrimSpace(credentials.APIKey),
			"apiSecret": strings.TrimSpace(credentials.APISecret),
		}
	default:
		if authType == "api_key" {
			payload = map[string]string{
				"email":  strings.TrimSpace(credentials.APIEmail),
				"apiKey": strings.TrimSpace(credentials.APIKey),
			}
		} else {
			payload = map[string]string{
				"apiToken": strings.TrimSpace(credentials.APIToken),
				"email":    strings.TrimSpace(credentials.APIEmail),
				"apiKey":   strings.TrimSpace(credentials.APIKey),
			}
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

func hasProviderCredentials(credentials ProviderCredentialsInput) bool {
	return strings.TrimSpace(credentials.APIToken) != "" ||
		strings.TrimSpace(credentials.APIEmail) != "" ||
		strings.TrimSpace(credentials.APIKey) != "" ||
		strings.TrimSpace(credentials.APISecret) != ""
}

func (s *Service) ValidateProviderAccount(ctx context.Context, providerAccountID uint64) (ProviderAccount, error) {
	item, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return ProviderAccount{}, err
	}
	if s.providers == nil {
		return ProviderAccount{}, ErrProviderAdapterUnavailable
	}

	adapter, ok := s.providers.Get(item.Provider)
	if !ok {
		return ProviderAccount{}, ErrProviderAdapterUnavailable
	}

	result, err := adapter.Validate(ctx, domainprovider.Account{
		ID:        item.ID,
		Provider:  item.Provider,
		AuthType:  item.AuthType,
		SecretRef: item.SecretRef,
	})
	if err != nil {
		return ProviderAccount{}, err
	}

	now := time.Now()
	item.Status = result.Status
	item.Capabilities = sanitizeCapabilities(result.Capabilities)
	item.LastSyncAt = &now
	return s.repo.UpdateProviderAccount(ctx, item)
}

func (s *Service) ValidateOwnedProviderAccount(ctx context.Context, userID uint64, providerAccountID uint64) (ProviderAccount, error) {
	item, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return ProviderAccount{}, err
	}
	if item.OwnerUserID == nil || *item.OwnerUserID != userID {
		return ProviderAccount{}, ErrProviderAccountNotFound
	}
	return s.ValidateProviderAccount(ctx, providerAccountID)
}

func (s *Service) UpdateOwnedProviderAccount(ctx context.Context, userID uint64, providerAccountID uint64, req CreateProviderAccountRequest) (ProviderAccount, error) {
	existing, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return ProviderAccount{}, err
	}
	if existing.OwnerUserID == nil || *existing.OwnerUserID != userID {
		return ProviderAccount{}, ErrProviderAccountNotFound
	}

	bound, err := s.ownedProviderHasBoundDomains(ctx, userID, providerAccountID)
	if err != nil {
		return ProviderAccount{}, err
	}

	nextProvider := strings.TrimSpace(req.Provider)
	if nextProvider == "" {
		nextProvider = existing.Provider
	}
	nextAuthType := strings.TrimSpace(req.AuthType)
	if nextAuthType == "" {
		nextAuthType = existing.AuthType
	}

	if bound && (nextProvider != existing.Provider || nextAuthType != existing.AuthType) {
		return ProviderAccount{}, ErrProviderAccountImmutableFieldsLocked
	}

	req.Provider = nextProvider
	req.AuthType = nextAuthType
	req.OwnerType = existing.OwnerType
	req.OwnerUserID = existing.OwnerUserID
	return s.UpdateProviderAccount(ctx, providerAccountID, req)
}

func (s *Service) ListProviderZones(ctx context.Context, providerAccountID uint64) ([]ProviderZone, error) {
	item, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return nil, err
	}
	if s.providers == nil {
		return nil, ErrProviderAdapterUnavailable
	}

	adapter, ok := s.providers.Get(item.Provider)
	if !ok {
		return nil, ErrProviderAdapterUnavailable
	}

	zones, err := adapter.ListZones(ctx, domainprovider.Account{
		ID:        item.ID,
		Provider:  item.Provider,
		AuthType:  item.AuthType,
		SecretRef: item.SecretRef,
	})
	if err != nil {
		return nil, err
	}

	items := make([]ProviderZone, 0, len(zones))
	for _, zone := range zones {
		items = append(items, ProviderZone{
			ID:     zone.ID,
			Name:   zone.Name,
			Status: zone.Status,
		})
	}
	return items, nil
}

func (s *Service) ListOwnedProviderZones(ctx context.Context, userID uint64, providerAccountID uint64) ([]ProviderZone, error) {
	item, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return nil, err
	}
	if item.OwnerUserID == nil || *item.OwnerUserID != userID {
		return nil, ErrProviderAccountNotFound
	}
	return s.ListProviderZones(ctx, providerAccountID)
}

func (s *Service) ListProviderRecords(ctx context.Context, providerAccountID uint64, zoneID string) ([]ProviderRecord, error) {
	item, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return nil, err
	}
	if s.providers == nil {
		return nil, ErrProviderAdapterUnavailable
	}

	adapter, ok := s.providers.Get(item.Provider)
	if !ok {
		return nil, ErrProviderAdapterUnavailable
	}

	records, err := adapter.ListRecords(ctx, domainprovider.Account{
		ID:        item.ID,
		Provider:  item.Provider,
		AuthType:  item.AuthType,
		SecretRef: item.SecretRef,
	}, strings.TrimSpace(zoneID))
	if err != nil {
		return nil, err
	}

	items := make([]ProviderRecord, 0, len(records))
	for _, record := range records {
		items = append(items, ProviderRecord{
			ID:       record.ID,
			Type:     record.Type,
			Name:     record.Name,
			Value:    record.Value,
			TTL:      record.TTL,
			Priority: record.Priority,
			Proxied:  record.Proxied,
		})
	}
	return items, nil
}

func (s *Service) ListOwnedProviderRecords(ctx context.Context, userID uint64, providerAccountID uint64, zoneID string) ([]ProviderRecord, error) {
	item, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return nil, err
	}
	if item.OwnerUserID == nil || *item.OwnerUserID != userID {
		return nil, ErrProviderAccountNotFound
	}
	return s.ListProviderRecords(ctx, providerAccountID, zoneID)
}

func (s *Service) ListOwnedProviderChangeSets(ctx context.Context, userID uint64, providerAccountID uint64, zoneID string) ([]DNSChangeSet, error) {
	item, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return nil, err
	}
	if item.OwnerUserID == nil || *item.OwnerUserID != userID {
		return nil, ErrProviderAccountNotFound
	}
	return s.ListProviderChangeSets(ctx, providerAccountID, zoneID)
}

func (s *Service) PreviewOwnedProviderVerifications(ctx context.Context, userID uint64, providerAccountID uint64, zoneID string, zoneName string) ([]VerificationProfile, error) {
	item, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return nil, err
	}
	if item.OwnerUserID == nil || *item.OwnerUserID != userID {
		return nil, ErrProviderAccountNotFound
	}
	targetKind := s.verificationKindForZoneName(ctx, zoneName, &userID)
	return s.previewProviderVerifications(ctx, providerAccountID, zoneID, zoneName, targetKind)
}

func (s *Service) PreviewOwnedProviderChangeSet(ctx context.Context, userID uint64, providerAccountID uint64, zoneID string, req PreviewProviderChangeSetRequest) (DNSChangeSet, error) {
	item, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return DNSChangeSet{}, err
	}
	if item.OwnerUserID == nil || *item.OwnerUserID != userID {
		return DNSChangeSet{}, ErrProviderAccountNotFound
	}
	return s.PreviewProviderChangeSet(ctx, providerAccountID, zoneID, userID, req)
}

func (s *Service) ApplyOwnedProviderChangeSet(ctx context.Context, userID uint64, changeSetID uint64) (DNSChangeSet, error) {
	changeSet, err := s.repo.GetDNSChangeSetByID(ctx, changeSetID)
	if err != nil {
		return DNSChangeSet{}, err
	}

	account, err := s.repo.GetProviderAccountByID(ctx, changeSet.ProviderAccountID)
	if err != nil {
		return DNSChangeSet{}, err
	}
	if account.OwnerUserID == nil || *account.OwnerUserID != userID {
		return DNSChangeSet{}, ErrProviderAccountNotFound
	}

	return s.ApplyProviderChangeSet(ctx, changeSetID)
}

func (s *Service) Create(ctx context.Context, req CreateDomainRequest) (Domain, error) {
	hostname := normalizeDomain(req.Domain)
	if hostname == "" || strings.Contains(hostname, "..") || !strings.Contains(hostname, ".") {
		return Domain{}, ErrInvalidDomain
	}
	return s.repo.Upsert(ctx, Domain{
		Domain:            hostname,
		Status:            defaultStatus(req.Status),
		Visibility:        defaultVisibility(req.Visibility),
		PublicationStatus: defaultPublicationStatus(req.PublicationStatus),
		VerificationScore: defaultVerificationScore(req.VerificationScore),
		HealthStatus:      defaultHealthStatus(req.HealthStatus),
		ProviderAccountID: req.ProviderAccountID,
		IsDefault:         req.IsDefault,
		Weight:            defaultWeight(req.Weight),
	})
}

func (s *Service) CreateOwned(ctx context.Context, userID uint64, req CreateDomainRequest, apiKeys ...portal.APIKey) (Domain, error) {
	apiKey := optionalAPIKey(apiKeys...)
	if apiKey != nil {
		ownerUserID := userID
		if !portal.APIKeyAllowsResourceClass(*apiKey, userID, portal.BoundResource{
			Visibility:  "private",
			OwnerUserID: &ownerUserID,
		}) {
			return Domain{}, portal.ErrAPIKeyForbidden
		}
	}

	hostname := normalizeDomain(req.Domain)
	if hostname == "" || strings.Contains(hostname, "..") || !strings.Contains(hostname, ".") {
		return Domain{}, ErrInvalidDomain
	}
	existing, err := s.repo.FindByDomain(ctx, hostname)
	if err == nil {
		if existing.OwnerUserID != nil && *existing.OwnerUserID == userID {
			return Domain{}, ErrDomainAlreadyExists
		}
		return Domain{}, ErrDomainAlreadyExists
	}
	if !errors.Is(err, ErrDomainNotFound) {
		return Domain{}, err
	}

	ownerUserID := userID
	return s.repo.Upsert(ctx, Domain{
		Domain:            hostname,
		Status:            defaultStatus(req.Status),
		OwnerUserID:       &ownerUserID,
		Visibility:        "private",
		PublicationStatus: "draft",
		VerificationScore: defaultVerificationScore(req.VerificationScore),
		HealthStatus:      defaultHealthStatus(req.HealthStatus),
		ProviderAccountID: req.ProviderAccountID,
		IsDefault:         req.IsDefault,
		Weight:            defaultWeight(req.Weight),
	})
}

func (s *Service) DeleteOwned(ctx context.Context, userID uint64, domainID uint64) error {
	target, err := s.repo.GetActiveByID(ctx, domainID)
	if err != nil {
		return err
	}
	if target.OwnerUserID == nil || *target.OwnerUserID != userID {
		return ErrDomainNotFound
	}

	allDomains, err := s.repo.ListAll(ctx)
	if err != nil {
		return err
	}
	deleteTargets := []Domain{target}
	for _, item := range allDomains {
		if item.ID == target.ID {
			continue
		}
		if !strings.HasSuffix(item.Domain, "."+target.Domain) {
			continue
		}
		if item.OwnerUserID == nil || *item.OwnerUserID != userID {
			return ErrDomainHasChildren
		}
		deleteTargets = append(deleteTargets, item)
	}
	sort.Slice(deleteTargets, func(i, j int) bool {
		if deleteTargets[i].Level == deleteTargets[j].Level {
			return deleteTargets[i].ID > deleteTargets[j].ID
		}
		return deleteTargets[i].Level > deleteTargets[j].Level
	})

	for _, item := range deleteTargets {
		if s.hasMailboxes != nil {
			inUse, err := s.hasMailboxes(ctx, item.ID)
			if err != nil {
				return err
			}
			if inUse {
				return ErrDomainHasMailboxes
			}
		}
		if s.cleanupMailboxes != nil {
			if err := s.cleanupMailboxes(ctx, item.ID); err != nil {
				return err
			}
		}
	}

	for _, item := range deleteTargets {
		if err := s.repo.DeleteDomain(ctx, item.ID); err != nil {
			return err
		}
	}
	s.invalidateDomainCaches(ctx)
	return nil
}

func (s *Service) UpdateOwnedDomainProviderBinding(ctx context.Context, userID uint64, domainID uint64, req UpdateOwnedDomainProviderBindingRequest, apiKeys ...portal.APIKey) (Domain, error) {
	target, err := s.repo.GetActiveByID(ctx, domainID)
	if err != nil {
		return Domain{}, err
	}
	if target.OwnerUserID == nil || *target.OwnerUserID != userID {
		return Domain{}, ErrDomainNotFound
	}
	if apiKey := optionalAPIKey(apiKeys...); apiKey != nil && !portal.APIKeyAllowsBoundResource(*apiKey, userID, boundResourceFromDomain(target), "write") {
		return Domain{}, portal.ErrAPIKeyForbidden
	}

	if req.ProviderAccountID != nil {
		account, err := s.repo.GetProviderAccountByID(ctx, *req.ProviderAccountID)
		if err != nil {
			return Domain{}, err
		}
		if account.OwnerUserID == nil || *account.OwnerUserID != userID {
			return Domain{}, ErrProviderAccountNotFound
		}
	}

	if providerAccountIDsEqual(target.ProviderAccountID, req.ProviderAccountID) {
		return target, nil
	}

	target.ProviderAccountID = req.ProviderAccountID
	target.HealthStatus = defaultHealthStatus("")
	target.VerificationScore = 0

	updated, err := s.repo.Upsert(ctx, target)
	if err != nil {
		return Domain{}, err
	}
	s.invalidateDomainCaches(ctx)
	if s.auditRepo != nil {
		detail := map[string]any{
			"domainId": updated.ID,
		}
		if updated.ProviderAccountID != nil {
			detail["providerAccountId"] = *updated.ProviderAccountID
		} else {
			detail["providerAccountId"] = nil
		}
		_, _ = s.auditRepo.Create(ctx, userID, "domain.provider_binding.update", "domain", updated.Domain, detail)
	}
	return updated, nil
}

func (s *Service) deleteOwnedDomainWithoutChildrenCheck(ctx context.Context, target Domain) error {
	if err := s.repo.DeleteDomain(ctx, target.ID); err != nil {
		return err
	}
	s.invalidateDomainCaches(ctx)
	return nil
}

func (s *Service) GenerateSubdomains(ctx context.Context, req GenerateSubdomainsRequest) ([]Domain, error) {
	baseDomain, err := s.repo.GetActiveByID(ctx, req.BaseDomainID)
	if err != nil {
		return nil, err
	}

	items := make([]Domain, 0, len(req.Prefixes))
	for _, rawPrefix := range req.Prefixes {
		prefix := normalizeDomain(rawPrefix)
		prefix = strings.TrimSuffix(prefix, "."+baseDomain.Domain)
		if prefix == "" {
			continue
		}

		hostname := prefix + "." + baseDomain.Domain
		created, err := s.repo.Upsert(ctx, Domain{
			Domain:            hostname,
			Status:            defaultStatus(req.Status),
			Visibility:        inheritOrDefault(req.Visibility, baseDomain.Visibility, defaultVisibility("")),
			PublicationStatus: inheritOrDefault(req.PublicationStatus, baseDomain.PublicationStatus, defaultPublicationStatus("")),
			VerificationScore: defaultVerificationScore(req.VerificationScore),
			HealthStatus:      defaultHealthStatus(req.HealthStatus),
			ProviderAccountID: inheritProviderAccountID(req.ProviderAccountID, baseDomain.ProviderAccountID),
			IsDefault:         false,
			Weight:            defaultWeight(req.Weight),
		})
		if err != nil {
			return nil, err
		}
		items = append(items, created)
	}
	return items, nil
}

func (s *Service) GenerateOwnedSubdomains(ctx context.Context, userID uint64, req GenerateSubdomainsRequest, apiKeys ...portal.APIKey) ([]Domain, error) {
	baseDomain, err := s.repo.GetActiveByID(ctx, req.BaseDomainID)
	if err != nil {
		return nil, err
	}
	if baseDomain.OwnerUserID == nil || *baseDomain.OwnerUserID != userID {
		return nil, ErrDomainNotFound
	}
	if apiKey := optionalAPIKey(apiKeys...); apiKey != nil && !portal.APIKeyAllowsBoundResource(*apiKey, userID, boundResourceFromDomain(baseDomain), "write") {
		return nil, portal.ErrAPIKeyForbidden
	}

	items := make([]Domain, 0, len(req.Prefixes))
	for _, rawPrefix := range req.Prefixes {
		prefix := normalizeDomain(rawPrefix)
		prefix = strings.TrimSuffix(prefix, "."+baseDomain.Domain)
		if prefix == "" {
			continue
		}

		hostname := prefix + "." + baseDomain.Domain
		created, err := s.repo.Upsert(ctx, Domain{
			Domain:            hostname,
			Status:            defaultStatus(req.Status),
			OwnerUserID:       baseDomain.OwnerUserID,
			Visibility:        inheritOrDefault(req.Visibility, baseDomain.Visibility, defaultVisibility("")),
			PublicationStatus: inheritOrDefault(req.PublicationStatus, baseDomain.PublicationStatus, defaultPublicationStatus("")),
			VerificationScore: defaultVerificationScore(req.VerificationScore),
			HealthStatus:      defaultHealthStatus(req.HealthStatus),
			ProviderAccountID: inheritProviderAccountID(req.ProviderAccountID, baseDomain.ProviderAccountID),
			IsDefault:         false,
			Weight:            defaultWeight(req.Weight),
		})
		if err != nil {
			return nil, err
		}
		items = append(items, created)
	}
	return items, nil
}

func (s *Service) DeleteOwnedProviderAccount(ctx context.Context, userID uint64, providerAccountID uint64) error {
	account, err := s.repo.GetProviderAccountByID(ctx, providerAccountID)
	if err != nil {
		return err
	}
	if account.OwnerUserID == nil || *account.OwnerUserID != userID {
		return ErrProviderAccountNotFound
	}

	bound, err := s.ownedProviderHasBoundDomains(ctx, userID, providerAccountID)
	if err != nil {
		return err
	}
	if bound {
		return ErrProviderAccountInUse
	}

	return s.repo.DeleteProviderAccount(ctx, providerAccountID)
}

func (s *Service) ownedProviderHasBoundDomains(ctx context.Context, userID uint64, providerAccountID uint64) (bool, error) {
	domains, err := s.repo.ListAll(ctx)
	if err != nil {
		return false, err
	}
	for _, item := range domains {
		if item.OwnerUserID == nil || *item.OwnerUserID != userID {
			continue
		}
		if item.ProviderAccountID != nil && *item.ProviderAccountID == providerAccountID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) RequestPublicPoolPublication(ctx context.Context, userID uint64, domainID uint64, apiKeys ...portal.APIKey) (Domain, error) {
	item, err := s.repo.GetActiveByID(ctx, domainID)
	if err != nil {
		return Domain{}, err
	}
	if item.OwnerUserID == nil || *item.OwnerUserID != userID {
		return Domain{}, ErrDomainNotFound
	}
	if apiKey := optionalAPIKey(apiKeys...); apiKey != nil && !portal.APIKeyAllowsBoundResource(*apiKey, userID, boundResourceFromDomain(item), "publish") {
		return Domain{}, portal.ErrAPIKeyForbidden
	}
	if item.Visibility == "platform_public" {
		return Domain{}, ErrInvalidPublicationState
	}

	policy := s.loadDomainPublicPoolPolicy(ctx)
	item.Visibility = "public_pool"
	if policy.RequiresReview {
		item.PublicationStatus = "pending_review"
	} else {
		item.PublicationStatus = "approved"
	}

	updated, err := s.repo.Upsert(ctx, item)
	if err != nil {
		return Domain{}, err
	}
	s.invalidateDomainCaches(ctx)
	if s.auditRepo != nil {
		action := "domain.public_pool.request"
		if !policy.RequiresReview {
			action = "domain.public_pool.auto_approve"
		}
		_, _ = s.auditRepo.Create(ctx, userID, action, "domain", updated.Domain, map[string]any{
			"visibility":        updated.Visibility,
			"publicationStatus": updated.PublicationStatus,
			"requiresReview":    policy.RequiresReview,
		})
	}
	return updated, nil
}

func (s *Service) WithdrawPublicPoolPublication(ctx context.Context, userID uint64, domainID uint64, apiKeys ...portal.APIKey) (Domain, error) {
	item, err := s.repo.GetActiveByID(ctx, domainID)
	if err != nil {
		return Domain{}, err
	}
	if item.OwnerUserID == nil || *item.OwnerUserID != userID {
		return Domain{}, ErrDomainNotFound
	}
	if apiKey := optionalAPIKey(apiKeys...); apiKey != nil && !portal.APIKeyAllowsBoundResource(*apiKey, userID, boundResourceFromDomain(item), "publish") {
		return Domain{}, portal.ErrAPIKeyForbidden
	}
	if item.Visibility != "public_pool" {
		return Domain{}, ErrInvalidPublicationState
	}

	item.Visibility = "private"
	item.PublicationStatus = "draft"
	updated, err := s.repo.Upsert(ctx, item)
	if err != nil {
		return Domain{}, err
	}
	s.invalidateDomainCaches(ctx)
	if s.auditRepo != nil {
		_, _ = s.auditRepo.Create(ctx, userID, "domain.public_pool.withdraw", "domain", updated.Domain, map[string]any{
			"visibility":        updated.Visibility,
			"publicationStatus": updated.PublicationStatus,
		})
	}
	return updated, nil
}

func defaultStatus(value string) string {
	if strings.TrimSpace(value) == "" {
		return "active"
	}
	return strings.TrimSpace(value)
}

func defaultWeight(value int) int {
	if value <= 0 {
		return 100
	}
	return value
}

func defaultVisibility(value string) string {
	if strings.TrimSpace(value) == "" {
		return "private"
	}
	return strings.TrimSpace(value)
}

func defaultPublicationStatus(value string) string {
	if strings.TrimSpace(value) == "" {
		return "draft"
	}
	return strings.TrimSpace(value)
}

func defaultVerificationScore(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func defaultHealthStatus(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return strings.TrimSpace(value)
}

func defaultOwnerType(value string) string {
	if strings.TrimSpace(value) == "" {
		return "platform"
	}
	return strings.TrimSpace(value)
}

func defaultAuthType(value string) string {
	if strings.TrimSpace(value) == "" {
		return "api_token"
	}
	return strings.TrimSpace(value)
}

func defaultProviderStatus(value string) string {
	if strings.TrimSpace(value) == "" {
		return "pending"
	}
	return strings.TrimSpace(value)
}

func sanitizeCapabilities(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	output := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		output = append(output, trimmed)
	}
	return output
}

func inheritProviderAccountID(value *uint64, inherited *uint64) *uint64 {
	if value != nil {
		return value
	}
	return inherited
}

func providerAccountIDsEqual(left *uint64, right *uint64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func inheritOrDefault(value string, inherited string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if strings.TrimSpace(inherited) != "" {
		return strings.TrimSpace(inherited)
	}
	return fallback
}

func normalizeDomain(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
}

func IsRootDomainHostname(value string) bool {
	hostname := normalizeDomain(value)
	if hostname == "" {
		return false
	}
	_, _, level, kind := classifyDomain(hostname)
	return kind == "root" && level == 0
}

func optionalAPIKey(items ...portal.APIKey) *portal.APIKey {
	if len(items) == 0 {
		return nil
	}
	return &items[0]
}

func boundResourceFromDomain(item Domain) portal.BoundResource {
	return portal.BoundResource{
		NodeID:            &item.ID,
		Visibility:        item.Visibility,
		PublicationStatus: item.PublicationStatus,
		OwnerUserID:       item.OwnerUserID,
	}
}

func (s *Service) loadDomainPublicPoolPolicy(ctx context.Context) system.DomainPublicPoolPolicy {
	policy := system.DomainPublicPoolPolicy{RequiresReview: true}
	if s.configRepo == nil {
		return policy
	}

	items, err := s.configRepo.List(ctx)
	if err != nil {
		return policy
	}
	for _, item := range items {
		if item.Key != system.ConfigKeyDomainPublicPoolPolicy {
			continue
		}
		if raw, ok := item.Value["requiresReview"].(bool); ok {
			policy.RequiresReview = raw
		}
		return policy
	}
	return policy
}

func optionalJSONCache(items []*sharedcache.JSONCache) *sharedcache.JSONCache {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func (s *Service) invalidateDomainCaches(ctx context.Context) {
	if s.cache == nil {
		return
	}
	_ = s.cache.Delete(ctx, "cache:admin:overview")
	_ = s.cache.DeleteByPattern(ctx, "cache:dashboard:user:*")
}
