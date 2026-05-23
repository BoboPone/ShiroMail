package domain

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domainprovider "shiro-email/backend/internal/modules/domain/provider"
	"shiro-email/backend/internal/modules/system"
)

func TestDeleteOwnedRejectsMailboxBoundDomain(t *testing.T) {
	t.Parallel()

	repo := NewMemoryRepository(nil)
	service := NewService(repo, func(_ context.Context, domainID uint64) (bool, error) {
		return domainID != 0, nil
	}, nil, nil, nil, nil)

	userID := uint64(7)
	owned, err := service.CreateOwned(context.Background(), userID, CreateDomainRequest{
		Domain: "owned-delete-check.test",
	})
	if err != nil {
		t.Fatalf("create owned domain: %v", err)
	}

	err = service.DeleteOwned(context.Background(), userID, owned.ID)
	if !errors.Is(err, ErrDomainHasMailboxes) {
		t.Fatalf("expected ErrDomainHasMailboxes, got %v", err)
	}
}

func TestDeleteOwnedCleansInactiveMailboxesBeforeDelete(t *testing.T) {
	t.Parallel()

	repo := NewMemoryRepository(nil)
	var cleanupCalls atomic.Int32
	service := NewService(repo, func(_ context.Context, _ uint64) (bool, error) {
		return false, nil
	}, func(_ context.Context, domainID uint64) error {
		if domainID != 0 {
			cleanupCalls.Add(1)
		}
		return nil
	}, nil, nil, nil)

	userID := uint64(7)
	owned, err := service.CreateOwned(context.Background(), userID, CreateDomainRequest{
		Domain: "owned-cleanup-check.test",
	})
	if err != nil {
		t.Fatalf("create owned domain: %v", err)
	}

	if err := service.DeleteOwned(context.Background(), userID, owned.ID); err != nil {
		t.Fatalf("delete owned domain: %v", err)
	}
	if cleanupCalls.Load() != 1 {
		t.Fatalf("expected cleanup callback to run once, got %d", cleanupCalls.Load())
	}
}

func TestDeleteOwnedDeletesUnusedSubdomainsTogether(t *testing.T) {
	t.Parallel()

	repo := NewMemoryRepository(nil)
	service := NewService(repo, func(_ context.Context, _ uint64) (bool, error) {
		return false, nil
	}, nil, nil, nil, nil)

	userID := uint64(7)
	root, err := service.CreateOwned(context.Background(), userID, CreateDomainRequest{
		Domain: "owned-delete-tree.test",
	})
	if err != nil {
		t.Fatalf("create owned root domain: %v", err)
	}

	items, err := service.GenerateOwnedSubdomains(context.Background(), userID, GenerateSubdomainsRequest{
		BaseDomainID: root.ID,
		Prefixes:     []string{"relay.cn.hk"},
	})
	if err != nil {
		t.Fatalf("generate owned subdomains: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one generated subdomain, got %d", len(items))
	}

	if err := service.DeleteOwned(context.Background(), userID, root.ID); err != nil {
		t.Fatalf("delete owned root domain with unused subdomain: %v", err)
	}

	all, err := repo.ListAll(context.Background())
	if err != nil {
		t.Fatalf("list domains after delete: %v", err)
	}
	for _, item := range all {
		if item.Domain == root.Domain || item.Domain == items[0].Domain {
			t.Fatalf("expected delete to remove descendant tree, found %q", item.Domain)
		}
	}
}

func TestCreateOwnedTreatsMultilevelManagedDomainAsRoot(t *testing.T) {
	t.Parallel()

	repo := NewMemoryRepository(nil)
	service := NewService(repo, nil, nil, nil, nil, nil)

	userID := uint64(7)
	root, err := service.CreateOwned(context.Background(), userID, CreateDomainRequest{
		Domain: "oom.fnrry.com",
	})
	if err != nil {
		t.Fatalf("create owned multilevel root domain: %v", err)
	}
	if root.Kind != "root" || root.RootDomain != "oom.fnrry.com" || root.Level != 0 {
		t.Fatalf("expected multilevel domain to be root, got %+v", root)
	}

	children, err := service.GenerateOwnedSubdomains(context.Background(), userID, GenerateSubdomainsRequest{
		BaseDomainID: root.ID,
		Prefixes:     []string{"mx"},
	})
	if err != nil {
		t.Fatalf("generate subdomain under multilevel root: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected one generated child, got %d", len(children))
	}

	child := children[0]
	if child.Domain != "mx.oom.fnrry.com" || child.Kind != "subdomain" || child.RootDomain != root.Domain || child.ParentDomain != root.Domain || child.Level != 1 {
		t.Fatalf("expected generated child to belong to multilevel root, got %+v", child)
	}
}

func TestPreviewOwnedProviderChangeSetRejectsForeignProviderAccount(t *testing.T) {
	t.Parallel()

	repo := NewMemoryRepository(nil)
	ownerID := uint64(9)
	account, err := repo.CreateProviderAccount(context.Background(), ProviderAccount{
		Provider:    "spaceship",
		OwnerType:   "user",
		OwnerUserID: &ownerID,
		DisplayName: "Foreign",
		AuthType:    "api_key",
		SecretRef:   `{"apiKey":"key","apiSecret":"secret"}`,
		Status:      "healthy",
	})
	if err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	service := NewService(repo, nil, nil, nil, nil, nil)
	_, err = service.PreviewOwnedProviderChangeSet(context.Background(), 7, account.ID, "example.com", PreviewProviderChangeSetRequest{
		ZoneName: "example.com",
		Records: []ProviderRecord{
			{Type: "MX", Name: "example.com", Value: "mx1.example.com", TTL: 120, Priority: 10},
		},
	})
	if !errors.Is(err, ErrProviderAccountNotFound) {
		t.Fatalf("expected ErrProviderAccountNotFound, got %v", err)
	}
}

func TestApplyOwnedProviderChangeSetRejectsForeignChangeSet(t *testing.T) {
	t.Parallel()

	repo := NewMemoryRepository(nil)
	ownerID := uint64(9)
	account, err := repo.CreateProviderAccount(context.Background(), ProviderAccount{
		Provider:    "spaceship",
		OwnerType:   "user",
		OwnerUserID: &ownerID,
		DisplayName: "Foreign",
		AuthType:    "api_key",
		SecretRef:   `{"apiKey":"key","apiSecret":"secret"}`,
		Status:      "healthy",
	})
	if err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	changeSet, err := repo.SaveDNSChangeSet(context.Background(), DNSChangeSet{
		ProviderAccountID: account.ID,
		ProviderZoneID:    "example.com",
		ZoneName:          "example.com",
		RequestedByUserID: ownerID,
		Status:            "previewed",
		Provider:          "spaceship",
		Summary:           "create 1, update 0, delete 0",
		Operations: []DNSChangeOperation{
			{
				Operation:  "create",
				RecordType: "MX",
				RecordName: "example.com",
				After:      &ProviderRecord{Type: "MX", Name: "example.com", Value: "mx1.example.com", TTL: 120, Priority: 10},
				Status:     "pending",
			},
		},
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("save change set: %v", err)
	}

	service := NewService(repo, nil, nil, nil, nil, nil)
	_, err = service.ApplyOwnedProviderChangeSet(context.Background(), 7, changeSet.ID)
	if !errors.Is(err, ErrProviderAccountNotFound) {
		t.Fatalf("expected ErrProviderAccountNotFound, got %v", err)
	}
}

func TestOwnedProviderChangeSetPreviewAndApply(t *testing.T) {
	t.Parallel()

	var listCalls atomic.Int32
	var putCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/dns/records/example.com":
			listCalls.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"items":[]}`))
		case request.Method == http.MethodPut && request.URL.Path == "/v1/dns/records/example.com":
			putCalls.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	repo := NewMemoryRepository(nil)
	ownerID := uint64(7)
	account, err := repo.CreateProviderAccount(context.Background(), ProviderAccount{
		Provider:    "spaceship",
		OwnerType:   "user",
		OwnerUserID: &ownerID,
		DisplayName: "Primary",
		AuthType:    "api_key",
		SecretRef:   `{"apiKey":"key","apiSecret":"secret"}`,
		Status:      "healthy",
	})
	if err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	registry := domainprovider.NewRegistry("https://unused.example", server.URL)
	service := NewService(repo, nil, nil, nil, nil, registry)

	preview, err := service.PreviewOwnedProviderChangeSet(context.Background(), ownerID, account.ID, "example.com", PreviewProviderChangeSetRequest{
		ZoneName: "example.com",
		Records: []ProviderRecord{
			{Type: "MX", Name: "example.com", Value: "mx1.example.com", TTL: 120, Priority: 10},
		},
	})
	if err != nil {
		t.Fatalf("preview owned change set: %v", err)
	}
	if preview.Status != "previewed" {
		t.Fatalf("expected previewed status, got %q", preview.Status)
	}
	if len(preview.Operations) != 1 || preview.Operations[0].Operation != "create" {
		t.Fatalf("expected create operation, got %#v", preview.Operations)
	}

	applied, err := service.ApplyOwnedProviderChangeSet(context.Background(), ownerID, preview.ID)
	if err != nil {
		t.Fatalf("apply owned change set: %v", err)
	}
	if applied.Status != "applied" {
		t.Fatalf("expected applied status, got %q", applied.Status)
	}
	if listCalls.Load() != 1 {
		t.Fatalf("expected 1 list records call, got %d", listCalls.Load())
	}
	if putCalls.Load() != 1 {
		t.Fatalf("expected 1 apply call, got %d", putCalls.Load())
	}
}

func TestApplyOwnedProviderChangeSetDoesNotPersistAppliedStatusWhenProviderRejectsMutation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/zones/zone-1/dns_records"):
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"success":true,"result":[]}`))
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/zones/zone-1/dns_records"):
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"success":false,"errors":[{"code":81057,"message":"record already exists"}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	repo := NewMemoryRepository(nil)
	ownerID := uint64(7)
	account, err := repo.CreateProviderAccount(context.Background(), ProviderAccount{
		Provider:    "cloudflare",
		OwnerType:   "user",
		OwnerUserID: &ownerID,
		DisplayName: "Primary",
		AuthType:    "api_token",
		SecretRef:   `{"apiToken":"cf-token"}`,
		Status:      "healthy",
	})
	if err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	registry := domainprovider.NewRegistry(server.URL, "https://unused.example")
	service := NewService(repo, nil, nil, nil, nil, registry)

	preview, err := service.PreviewOwnedProviderChangeSet(context.Background(), ownerID, account.ID, "zone-1", PreviewProviderChangeSetRequest{
		ZoneName: "example.com",
		Records: []ProviderRecord{
			{Type: "TXT", Name: "_acme-challenge.example.com", Value: "token-value", TTL: 120},
		},
	})
	if err != nil {
		t.Fatalf("preview owned change set: %v", err)
	}

	_, err = service.ApplyOwnedProviderChangeSet(context.Background(), ownerID, preview.ID)
	if err == nil {
		t.Fatal("expected apply owned change set to fail")
	}
	if !strings.Contains(err.Error(), "record already exists") {
		t.Fatalf("expected surfaced provider rejection, got %v", err)
	}

	saved, err := repo.GetDNSChangeSetByID(context.Background(), preview.ID)
	if err != nil {
		t.Fatalf("reload change set: %v", err)
	}
	if saved.Status != "previewed" {
		t.Fatalf("expected change set to remain previewed, got %q", saved.Status)
	}
	if saved.AppliedAt != nil {
		t.Fatalf("expected unapplied change set, got appliedAt=%v", saved.AppliedAt)
	}
}

func TestPreviewOwnedProviderChangeSetAcceptsNSRecords(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/zones/zone-1/dns_records"):
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"success":true,"result":[{"id":"ns-1","type":"NS","name":"www.example.com","content":"ns1.example.net","ttl":300,"priority":0,"proxied":false}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	repo := NewMemoryRepository(nil)
	ownerID := uint64(7)
	account, err := repo.CreateProviderAccount(context.Background(), ProviderAccount{
		Provider:    "cloudflare",
		OwnerType:   "user",
		OwnerUserID: &ownerID,
		DisplayName: "Primary",
		AuthType:    "api_token",
		SecretRef:   `{"apiToken":"cf-token"}`,
		Status:      "healthy",
	})
	if err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	registry := domainprovider.NewRegistry(server.URL, "https://unused.example")
	service := NewService(repo, nil, nil, nil, nil, registry)

	preview, err := service.PreviewOwnedProviderChangeSet(context.Background(), ownerID, account.ID, "zone-1", PreviewProviderChangeSetRequest{
		ZoneName: "example.com",
		Records: []ProviderRecord{
			{Type: "NS", Name: "www.example.com", Value: "ns1.example.net", TTL: 300},
		},
	})
	if err != nil {
		t.Fatalf("preview owned change set: %v", err)
	}
	if preview.Status != "previewed" {
		t.Fatalf("expected previewed status, got %q", preview.Status)
	}
	if len(preview.Operations) != 0 {
		t.Fatalf("expected no-op preview for unchanged NS record, got %#v", preview.Operations)
	}
}

func TestOwnedProviderChangeSetAllowsDeletingAllRecords(t *testing.T) {
	t.Parallel()

	var listCalls atomic.Int32
	var deleteCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/dns/records/example.com":
			listCalls.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"items":[{"type":"TXT","name":"@","value":"v=spf1 -all","ttl":120}]}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/v1/dns/records/example.com":
			deleteCalls.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`[]`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	repo := NewMemoryRepository(nil)
	ownerID := uint64(7)
	account, err := repo.CreateProviderAccount(context.Background(), ProviderAccount{
		Provider:    "spaceship",
		OwnerType:   "user",
		OwnerUserID: &ownerID,
		DisplayName: "Primary",
		AuthType:    "api_key",
		SecretRef:   `{"apiKey":"key","apiSecret":"secret"}`,
		Status:      "healthy",
	})
	if err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	registry := domainprovider.NewRegistry("https://unused.example", server.URL)
	service := NewService(repo, nil, nil, nil, nil, registry)

	preview, err := service.PreviewOwnedProviderChangeSet(context.Background(), ownerID, account.ID, "example.com", PreviewProviderChangeSetRequest{
		ZoneName: "example.com",
		Records:  []ProviderRecord{},
	})
	if err != nil {
		t.Fatalf("preview owned change set with empty records: %v", err)
	}
	if len(preview.Operations) != 1 || preview.Operations[0].Operation != "delete" {
		t.Fatalf("expected single delete operation, got %#v", preview.Operations)
	}

	applied, err := service.ApplyOwnedProviderChangeSet(context.Background(), ownerID, preview.ID)
	if err != nil {
		t.Fatalf("apply owned change set with empty records: %v", err)
	}
	if applied.Status != "applied" {
		t.Fatalf("expected applied status, got %q", applied.Status)
	}
	if listCalls.Load() != 1 {
		t.Fatalf("expected 1 list records call, got %d", listCalls.Load())
	}
	if deleteCalls.Load() != 1 {
		t.Fatalf("expected 1 delete apply call, got %d", deleteCalls.Load())
	}
}

func TestGenerateOwnedSubdomainsResetsInheritedVerificationState(t *testing.T) {
	t.Parallel()

	repo := NewMemoryRepository(nil)
	service := NewService(repo, nil, nil, nil, nil, nil)
	ownerID := uint64(7)
	providerID := uint64(11)

	root, err := repo.Upsert(context.Background(), Domain{
		Domain:            "example.com",
		OwnerUserID:       &ownerID,
		Status:            "active",
		Visibility:        "private",
		PublicationStatus: "draft",
		VerificationScore: 100,
		HealthStatus:      "healthy",
		ProviderAccountID: &providerID,
		Weight:            100,
	})
	if err != nil {
		t.Fatalf("seed root domain: %v", err)
	}

	items, err := service.GenerateOwnedSubdomains(context.Background(), ownerID, GenerateSubdomainsRequest{
		BaseDomainID: root.ID,
		Prefixes:     []string{"mx.edge"},
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("generate owned subdomains: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 generated subdomain, got %d", len(items))
	}
	if items[0].HealthStatus != "unknown" {
		t.Fatalf("expected generated subdomain health to reset to unknown, got %q", items[0].HealthStatus)
	}
	if items[0].VerificationScore != 0 {
		t.Fatalf("expected generated subdomain verification score to reset to 0, got %d", items[0].VerificationScore)
	}
}

func TestVerifyOwnedSubdomainUsesSubdomainSpecificRecords(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/zones":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"success":true,"result":[{"id":"zone-1","name":"example.com","status":"active"}]}`))
		case "/zones/zone-1/dns_records":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"success":true,"result":[{"id":"txt-1","type":"TXT","name":"_shiro-verification.example.com","content":"shiro-ownership=example.com","ttl":300,"priority":0,"proxied":false},{"id":"mx-1","type":"MX","name":"example.com","content":"mx1.example.com","ttl":300,"priority":10,"proxied":false},{"id":"spf-1","type":"TXT","name":"example.com","content":"v=spf1 mx -all","ttl":300,"priority":0,"proxied":false},{"id":"dkim-1","type":"CNAME","name":"shiro._domainkey.example.com","content":"shiro._domainkey.shiro.local","ttl":300,"priority":0,"proxied":false},{"id":"dmarc-1","type":"TXT","name":"_dmarc.example.com","content":"v=DMARC1; p=quarantine","ttl":300,"priority":0,"proxied":false}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	repo := NewMemoryRepository(nil)
	registry := domainprovider.NewRegistry(server.URL, "https://unused.example")
	service := NewService(repo, nil, nil, nil, nil, registry)
	ownerID := uint64(7)

	account, err := repo.CreateProviderAccount(context.Background(), ProviderAccount{
		Provider:    "cloudflare",
		OwnerType:   "user",
		OwnerUserID: &ownerID,
		DisplayName: "Primary",
		AuthType:    "api_token",
		SecretRef:   `{"apiToken":"cf-token"}`,
		Status:      "healthy",
	})
	if err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	root, err := repo.Upsert(context.Background(), Domain{
		Domain:            "example.com",
		OwnerUserID:       &ownerID,
		Status:            "active",
		Visibility:        "private",
		PublicationStatus: "draft",
		ProviderAccountID: &account.ID,
		Weight:            100,
	})
	if err != nil {
		t.Fatalf("seed root domain: %v", err)
	}

	items, err := service.GenerateOwnedSubdomains(context.Background(), ownerID, GenerateSubdomainsRequest{
		BaseDomainID: root.ID,
		Prefixes:     []string{"relay"},
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("generate owned subdomains: %v", err)
	}

	result, err := service.VerifyOwnedDomain(context.Background(), ownerID, items[0].ID)
	if err != nil {
		t.Fatalf("verify owned subdomain: %v", err)
	}
	if result.TotalCount != 1 || len(result.Profiles) != 1 || result.Profiles[0].VerificationType != "inbound_mx" {
		t.Fatalf("expected subdomain to require only MX verification, got %#v", result.Profiles)
	}
	if result.Passed {
		t.Fatalf("expected subdomain verification to fail when only root records exist: %#v", result)
	}
	if result.Domain.HealthStatus == "healthy" || result.Domain.VerificationScore == 100 {
		t.Fatalf("expected subdomain to remain unverified, got health=%q score=%d", result.Domain.HealthStatus, result.Domain.VerificationScore)
	}
}

func TestVerifyOwnedSubdomainWithoutProviderOnlyRequiresPublicMX(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := NewMemoryRepository(nil)
	configRepo := system.NewMemoryConfigRepository()
	_, err := configRepo.Upsert(ctx, system.ConfigKeyMailSMTP, map[string]any{
		"enabled":         true,
		"listenAddr":      ":2525",
		"hostname":        "mail.mmjs.top",
		"dkimCnameTarget": "shiro._domainkey.mmjs.top",
		"maxMessageBytes": 10485760,
	}, 1)
	if err != nil {
		t.Fatalf("seed smtp config: %v", err)
	}

	service := NewService(repo, nil, nil, configRepo, nil, nil)
	service.publicDNS = fakePublicDNSResolver{
		mx: map[string][]*net.MX{
			"relay.oom.fnrry.com": {{Host: "mail.mmjs.top.", Pref: 10}},
		},
	}

	ownerID := uint64(7)
	root, err := repo.Upsert(ctx, Domain{
		Domain:            "oom.fnrry.com",
		OwnerUserID:       &ownerID,
		Status:            "active",
		Visibility:        "private",
		PublicationStatus: "draft",
		HealthStatus:      "unknown",
		Weight:            100,
	})
	if err != nil {
		t.Fatalf("seed root domain: %v", err)
	}

	items, err := service.GenerateOwnedSubdomains(ctx, ownerID, GenerateSubdomainsRequest{
		BaseDomainID: root.ID,
		Prefixes:     []string{"relay"},
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("generate owned subdomain: %v", err)
	}

	result, err := service.VerifyOwnedDomain(ctx, ownerID, items[0].ID)
	if err != nil {
		t.Fatalf("verify owned subdomain: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected subdomain public MX verification to pass without ownership TXT: %#v", result)
	}
	if result.TotalCount != 1 || result.VerifiedCount != 1 || len(result.Profiles) != 1 || result.Profiles[0].VerificationType != "inbound_mx" {
		t.Fatalf("expected only inbound_mx verification for subdomain, got %d/%d %#v", result.VerifiedCount, result.TotalCount, result.Profiles)
	}
	if result.Domain.HealthStatus != "healthy" || result.Domain.VerificationScore != 100 {
		t.Fatalf("expected subdomain healthy with score 100, got health=%q score=%d", result.Domain.HealthStatus, result.Domain.VerificationScore)
	}
}

func TestVerifyDomainWithoutProviderUsesPublicDNS(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := NewMemoryRepository(nil)
	configRepo := system.NewMemoryConfigRepository()
	_, err := configRepo.Upsert(ctx, system.ConfigKeyMailSMTP, map[string]any{
		"enabled":         true,
		"listenAddr":      ":2525",
		"hostname":        "mail.mmjs.top",
		"dkimCnameTarget": "shiro._domainkey.mmjs.top",
		"maxMessageBytes": 10485760,
	}, 1)
	if err != nil {
		t.Fatalf("seed smtp config: %v", err)
	}

	service := NewService(repo, nil, nil, configRepo, nil, nil)
	service.publicDNS = fakePublicDNSResolver{
		mx: map[string][]*net.MX{
			"oom.fnrry.com": {{Host: "mail.mmjs.top.", Pref: 0}},
		},
		txt: map[string][]string{
			"_shiro-verification.oom.fnrry.com": {"shiro-ownership=oom.fnrry.com"},
		},
	}

	domain, err := repo.Upsert(ctx, Domain{
		Domain:            "oom.fnrry.com",
		Status:            "active",
		Visibility:        "private",
		PublicationStatus: "draft",
		HealthStatus:      "unknown",
		Weight:            100,
	})
	if err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	result, err := service.VerifyDomain(ctx, domain.ID)
	if err != nil {
		t.Fatalf("verify domain: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected public dns verification to pass: %#v", result)
	}
	if result.Domain.HealthStatus != "healthy" || result.Domain.VerificationScore != 100 {
		t.Fatalf("expected domain healthy with score 100, got health=%q score=%d", result.Domain.HealthStatus, result.Domain.VerificationScore)
	}
	if result.TotalCount != 2 || result.VerifiedCount != 2 {
		t.Fatalf("expected both required verification profiles to pass, got %d/%d", result.VerifiedCount, result.TotalCount)
	}
}

type fakePublicDNSResolver struct {
	mx    map[string][]*net.MX
	txt   map[string][]string
	cname map[string]string
}

func (r fakePublicDNSResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	return r.mx[name], nil
}

func (r fakePublicDNSResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	return r.txt[name], nil
}

func (r fakePublicDNSResolver) LookupCNAME(_ context.Context, host string) (string, error) {
	return r.cname[host], nil
}
