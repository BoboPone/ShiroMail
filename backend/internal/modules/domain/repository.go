package domain

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrDomainNotFound = errors.New("domain not found")
var ErrProviderAccountNotFound = errors.New("provider account not found")
var ErrDNSChangeSetNotFound = errors.New("dns change set not found")

type Repository interface {
	ListAll(ctx context.Context) ([]Domain, error)
	ListActive(ctx context.Context) ([]Domain, error)
	ListAccessibleActive(ctx context.Context, userID uint64) ([]Domain, error)
	FindByDomain(ctx context.Context, hostname string) (Domain, error)
	GetActiveByID(ctx context.Context, id uint64) (Domain, error)
	GetAccessibleActiveByID(ctx context.Context, userID uint64, id uint64) (Domain, error)
	Upsert(ctx context.Context, item Domain) (Domain, error)
	DeleteDomain(ctx context.Context, id uint64) error
	ListProviderAccounts(ctx context.Context) ([]ProviderAccount, error)
	GetProviderAccountByID(ctx context.Context, id uint64) (ProviderAccount, error)
	CreateProviderAccount(ctx context.Context, item ProviderAccount) (ProviderAccount, error)
	UpdateProviderAccount(ctx context.Context, item ProviderAccount) (ProviderAccount, error)
	DeleteProviderAccount(ctx context.Context, id uint64) error
	ListDNSChangeSets(ctx context.Context, providerAccountID uint64, providerZoneID string) ([]DNSChangeSet, error)
	SaveDNSChangeSet(ctx context.Context, item DNSChangeSet) (DNSChangeSet, error)
	GetDNSChangeSetByID(ctx context.Context, id uint64) (DNSChangeSet, error)
	CountActive(ctx context.Context) int
}

type MemoryRepository struct {
	mu               sync.RWMutex
	nextID           uint64
	nextProviderID   uint64
	nextChangeSetID  uint64
	nextChangeOpID   uint64
	domains          []Domain
	providerAccounts []ProviderAccount
	changeSets       []DNSChangeSet
}

func NewMemoryRepository(seed []Domain) *MemoryRepository {
	if len(seed) == 0 {
		seed = []Domain{
			{
				ID:                1,
				Domain:            "shiro.local",
				Status:            "active",
				Visibility:        "platform_public",
				PublicationStatus: "published",
				HealthStatus:      "healthy",
				IsDefault:         true,
				Weight:            100,
			},
		}
	}

	cloned := make([]Domain, len(seed))
	copy(cloned, seed)

	nextID := uint64(1)
	for _, item := range cloned {
		if item.ID >= nextID {
			nextID = item.ID + 1
		}
	}

	return &MemoryRepository{
		nextID:          nextID,
		nextProviderID:  1,
		nextChangeSetID: 1,
		nextChangeOpID:  1,
		domains:         cloned,
	}
}

func (r *MemoryRepository) ListAll(_ context.Context) ([]Domain, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]Domain, len(r.domains))
	for index, item := range r.domains {
		items[index] = r.decorateDomainLocked(item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (r *MemoryRepository) ListActive(_ context.Context) ([]Domain, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	active := make([]Domain, 0, len(r.domains))
	for _, item := range r.domains {
		if item.Status == "active" {
			active = append(active, r.decorateDomainLocked(item))
		}
	}
	return active, nil
}

func (r *MemoryRepository) ListAccessibleActive(_ context.Context, userID uint64) ([]Domain, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	active := make([]Domain, 0, len(r.domains))
	for _, item := range r.domains {
		if item.Status != "active" {
			continue
		}
		decorated := r.decorateDomainLocked(item)
		if isAccessibleToUser(decorated, userID) {
			active = append(active, decorated)
		}
	}
	return active, nil
}

func (r *MemoryRepository) GetActiveByID(_ context.Context, id uint64) (Domain, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, item := range r.domains {
		if item.ID == id && item.Status == "active" {
			return r.decorateDomainLocked(item), nil
		}
	}
	return Domain{}, ErrDomainNotFound
}

func (r *MemoryRepository) GetAccessibleActiveByID(_ context.Context, userID uint64, id uint64) (Domain, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, item := range r.domains {
		if item.ID != id || item.Status != "active" {
			continue
		}
		decorated := r.decorateDomainLocked(item)
		if !isAccessibleToUser(decorated, userID) {
			return Domain{}, ErrDomainNotFound
		}
		return decorated, nil
	}
	return Domain{}, ErrDomainNotFound
}

func (r *MemoryRepository) FindByDomain(_ context.Context, hostname string) (Domain, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, item := range r.domains {
		if item.Domain == hostname {
			return r.decorateDomainLocked(item), nil
		}
	}
	return Domain{}, ErrDomainNotFound
}

func (r *MemoryRepository) Upsert(_ context.Context, item Domain) (Domain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	item = applyDomainDefaults(item)
	for index, existing := range r.domains {
		if existing.ID == item.ID && item.ID != 0 {
			item.ID = existing.ID
			item.OwnerUserID = preserveOwner(existing.OwnerUserID, item.OwnerUserID)
			item = r.decorateDomainLocked(item)
			r.domains[index] = item
			return item, nil
		}
		if existing.Domain == item.Domain {
			item.ID = existing.ID
			item.OwnerUserID = preserveOwner(existing.OwnerUserID, item.OwnerUserID)
			item = r.decorateDomainLocked(item)
			r.domains[index] = item
			return item, nil
		}
	}

	item.ID = r.nextID
	r.nextID++
	item = r.decorateDomainLocked(item)
	r.domains = append(r.domains, item)
	return item, nil
}

func (r *MemoryRepository) DeleteDomain(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for index, item := range r.domains {
		if item.ID != id {
			continue
		}
		r.domains = append(r.domains[:index], r.domains[index+1:]...)
		return nil
	}
	return ErrDomainNotFound
}

func (r *MemoryRepository) ListProviderAccounts(_ context.Context) ([]ProviderAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]ProviderAccount, len(r.providerAccounts))
	for index, item := range r.providerAccounts {
		items[index] = sanitizeProviderAccount(item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID > items[j].ID
	})
	return items, nil
}

func (r *MemoryRepository) GetProviderAccountByID(_ context.Context, id uint64) (ProviderAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, item := range r.providerAccounts {
		if item.ID == id {
			return sanitizeProviderAccount(item), nil
		}
	}
	return ProviderAccount{}, ErrProviderAccountNotFound
}

func (r *MemoryRepository) CreateProviderAccount(_ context.Context, item ProviderAccount) (ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	item.ID = r.nextProviderID
	r.nextProviderID++
	now := time.Now()
	item.CreatedAt = now
	item.UpdatedAt = now
	if item.AuthType == "" {
		item.AuthType = "api_token"
	}
	if item.Status == "" {
		item.Status = "pending"
	}
	item.HasSecret = strings.TrimSpace(item.SecretRef) != ""
	r.providerAccounts = append(r.providerAccounts, item)
	return sanitizeProviderAccount(item), nil
}

func (r *MemoryRepository) UpdateProviderAccount(_ context.Context, item ProviderAccount) (ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for index, existing := range r.providerAccounts {
		if existing.ID != item.ID {
			continue
		}
		if strings.TrimSpace(item.SecretRef) == "" {
			item.SecretRef = existing.SecretRef
		}
		item.CreatedAt = existing.CreatedAt
		item.UpdatedAt = time.Now()
		item.HasSecret = strings.TrimSpace(item.SecretRef) != ""
		r.providerAccounts[index] = item
		return sanitizeProviderAccount(item), nil
	}
	return ProviderAccount{}, ErrProviderAccountNotFound
}

func (r *MemoryRepository) DeleteProviderAccount(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for index, item := range r.providerAccounts {
		if item.ID != id {
			continue
		}
		r.providerAccounts = append(r.providerAccounts[:index], r.providerAccounts[index+1:]...)
		return nil
	}
	return ErrProviderAccountNotFound
}

func (r *MemoryRepository) SaveDNSChangeSet(_ context.Context, item DNSChangeSet) (DNSChangeSet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if item.ID == 0 {
		item.ID = r.nextChangeSetID
		r.nextChangeSetID++
		if item.CreatedAt.IsZero() {
			item.CreatedAt = time.Now()
		}
		item.Operations = r.assignChangeOperationIDsLocked(item.ID, item.Operations)
		r.changeSets = append(r.changeSets, cloneDNSChangeSet(item))
		return cloneDNSChangeSet(item), nil
	}

	for index, existing := range r.changeSets {
		if existing.ID != item.ID {
			continue
		}
		if item.CreatedAt.IsZero() {
			item.CreatedAt = existing.CreatedAt
		}
		item.Operations = r.assignChangeOperationIDsLocked(item.ID, item.Operations)
		r.changeSets[index] = cloneDNSChangeSet(item)
		return cloneDNSChangeSet(r.changeSets[index]), nil
	}

	return DNSChangeSet{}, ErrDNSChangeSetNotFound
}

func (r *MemoryRepository) ListDNSChangeSets(_ context.Context, providerAccountID uint64, providerZoneID string) ([]DNSChangeSet, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]DNSChangeSet, 0, len(r.changeSets))
	for _, item := range r.changeSets {
		if item.ProviderAccountID != providerAccountID {
			continue
		}
		if strings.TrimSpace(providerZoneID) != "" && item.ProviderZoneID != strings.TrimSpace(providerZoneID) {
			continue
		}
		items = append(items, cloneDNSChangeSet(item))
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (r *MemoryRepository) GetDNSChangeSetByID(_ context.Context, id uint64) (DNSChangeSet, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, item := range r.changeSets {
		if item.ID == id {
			return cloneDNSChangeSet(item), nil
		}
	}
	return DNSChangeSet{}, ErrDNSChangeSetNotFound
}

func (r *MemoryRepository) decorateDomainLocked(item Domain) Domain {
	item = applyDomainDefaults(item)
	item.RootDomain, item.ParentDomain, item.Level, item.Kind = r.classifyDomainLocked(item)
	if item.ProviderAccountID == nil {
		return item
	}
	for _, provider := range r.providerAccounts {
		if provider.ID != *item.ProviderAccountID {
			continue
		}
		item.Provider = provider.Provider
		item.ProviderDisplayName = provider.DisplayName
		return item
	}
	return item
}

func (r *MemoryRepository) classifyDomainLocked(item Domain) (string, string, int, string) {
	nodes := make([]domainHierarchyNode, 0, len(r.domains))
	for _, candidate := range r.domains {
		nodes = append(nodes, domainHierarchyNode{
			ID:          candidate.ID,
			Domain:      candidate.Domain,
			OwnerUserID: candidate.OwnerUserID,
		})
	}
	return classifyManagedDomain(item.Domain, item.ID, item.OwnerUserID, nodes)
}

func (r *MemoryRepository) CountActive(_ context.Context) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, item := range r.domains {
		if item.Status == "active" {
			count++
		}
	}
	return count
}

func classifyDomain(hostname string) (rootDomain string, parentDomain string, level int, kind string) {
	return classifyManagedDomain(hostname, 0, nil, nil)
}

type domainHierarchyNode struct {
	ID          uint64
	Domain      string
	OwnerUserID *uint64
}

func classifyManagedDomain(hostname string, id uint64, ownerUserID *uint64, nodes []domainHierarchyNode) (rootDomain string, parentDomain string, level int, kind string) {
	normalized := strings.Trim(strings.ToLower(strings.TrimSpace(hostname)), ".")
	if normalized == "" {
		return "", "", 0, "root"
	}

	parts := strings.Split(normalized, ".")
	rootDomain = normalized
	for _, node := range nodes {
		candidate := strings.Trim(strings.ToLower(strings.TrimSpace(node.Domain)), ".")
		if candidate == "" || candidate == normalized {
			continue
		}
		if id != 0 && node.ID == id {
			continue
		}
		if !domainOwnersMatch(node.OwnerUserID, ownerUserID) {
			continue
		}
		if !isSubdomainOf(normalized, candidate) {
			continue
		}
		if rootDomain == normalized || labelCount(candidate) > labelCount(rootDomain) {
			rootDomain = candidate
		}
	}

	if rootDomain == normalized {
		return normalized, "", 0, "root"
	}

	parentDomain = strings.Join(parts[1:], ".")
	level = len(parts) - labelCount(rootDomain)
	kind = "subdomain"
	return rootDomain, parentDomain, level, kind
}

func domainOwnersMatch(left *uint64, right *uint64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func isSubdomainOf(hostname string, parent string) bool {
	return hostname != parent && strings.HasSuffix(hostname, "."+parent)
}

func labelCount(hostname string) int {
	if strings.TrimSpace(hostname) == "" {
		return 0
	}
	return len(strings.Split(hostname, "."))
}

func parentDomainCandidates(hostname string) []string {
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(hostname)), "."), ".")
	if len(parts) <= 2 {
		return nil
	}
	candidates := make([]string, 0, len(parts)-2)
	for index := 1; index <= len(parts)-2; index++ {
		candidates = append(candidates, strings.Join(parts[index:], "."))
	}
	return candidates
}

func applyDomainDefaults(item Domain) Domain {
	if strings.TrimSpace(item.Status) == "" {
		item.Status = "active"
	}
	if strings.TrimSpace(item.Visibility) == "" {
		item.Visibility = "private"
	}
	if strings.TrimSpace(item.PublicationStatus) == "" {
		item.PublicationStatus = "draft"
	}
	if strings.TrimSpace(item.HealthStatus) == "" {
		item.HealthStatus = "unknown"
	}
	if item.Weight <= 0 {
		item.Weight = 100
	}
	return item
}

func preserveOwner(existing *uint64, incoming *uint64) *uint64 {
	if incoming != nil {
		return incoming
	}
	return existing
}

func sanitizeProviderAccount(item ProviderAccount) ProviderAccount {
	item.HasSecret = strings.TrimSpace(item.SecretRef) != ""
	return item
}

func (r *MemoryRepository) assignChangeOperationIDsLocked(changeSetID uint64, operations []DNSChangeOperation) []DNSChangeOperation {
	items := make([]DNSChangeOperation, len(operations))
	for index, operation := range operations {
		if operation.ID == 0 {
			operation.ID = r.nextChangeOpID
			r.nextChangeOpID++
		}
		operation.ChangeSetID = changeSetID
		items[index] = cloneDNSChangeOperation(operation)
	}
	return items
}

func cloneDNSChangeSet(item DNSChangeSet) DNSChangeSet {
	cloned := item
	if item.ZoneID != nil {
		value := *item.ZoneID
		cloned.ZoneID = &value
	}
	if item.RequestedByAPIKeyID != nil {
		value := *item.RequestedByAPIKeyID
		cloned.RequestedByAPIKeyID = &value
	}
	cloned.Operations = make([]DNSChangeOperation, len(item.Operations))
	for index, operation := range item.Operations {
		cloned.Operations[index] = cloneDNSChangeOperation(operation)
	}
	return cloned
}

func cloneDNSChangeOperation(item DNSChangeOperation) DNSChangeOperation {
	cloned := item
	if item.Before != nil {
		record := *item.Before
		cloned.Before = &record
	}
	if item.After != nil {
		record := *item.After
		cloned.After = &record
	}
	return cloned
}

func isAccessibleToUser(item Domain, userID uint64) bool {
	if item.OwnerUserID != nil && *item.OwnerUserID == userID {
		return true
	}

	switch strings.TrimSpace(item.Visibility) {
	case "platform_public", "public_pool":
		return isPublishedForSharedAccess(item.PublicationStatus)
	default:
		return false
	}
}

func isPublishedForSharedAccess(status string) bool {
	switch strings.TrimSpace(status) {
	case "", "published", "approved", "active":
		return true
	default:
		return false
	}
}
