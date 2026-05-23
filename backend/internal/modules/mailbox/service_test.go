package mailbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"shiro-email/backend/internal/modules/domain"
)

func TestCreateMailboxRejectsUnverifiedSubdomain(t *testing.T) {
	t.Parallel()

	domainRepo := domain.NewMemoryRepository(nil)
	mailboxRepo := NewMemoryRepository()
	service := NewService(mailboxRepo, domainRepo)
	userID := uint64(7)

	root, err := domainRepo.Upsert(context.Background(), domain.Domain{
		Domain:            "example.com",
		OwnerUserID:       &userID,
		Status:            "active",
		Visibility:        "private",
		PublicationStatus: "draft",
		ProviderAccountID: pointerUint64(11),
		VerificationScore: 100,
		HealthStatus:      "healthy",
		Weight:            100,
	})
	if err != nil {
		t.Fatalf("seed root domain: %v", err)
	}

	child, err := domainRepo.Upsert(context.Background(), domain.Domain{
		Domain:            "relay.example.com",
		OwnerUserID:       &userID,
		Status:            "active",
		Visibility:        "private",
		PublicationStatus: "draft",
		ProviderAccountID: root.ProviderAccountID,
		VerificationScore: 0,
		HealthStatus:      "unknown",
		Weight:            90,
	})
	if err != nil {
		t.Fatalf("seed subdomain: %v", err)
	}

	_, err = service.CreateMailbox(context.Background(), userID, CreateMailboxRequest{
		DomainID:       child.ID,
		LocalPart:      "testbox",
		ExpiresInHours: 24,
	})
	if !errors.Is(err, ErrDomainVerificationRequired) {
		t.Fatalf("expected ErrDomainVerificationRequired, got %v", err)
	}
}

func TestBuildDashboardHidesUnverifiedSubdomainsFromAvailableDomains(t *testing.T) {
	t.Parallel()

	domainRepo := domain.NewMemoryRepository(nil)
	mailboxRepo := NewMemoryRepository()
	service := NewService(mailboxRepo, domainRepo)
	userID := uint64(7)

	_, err := domainRepo.Upsert(context.Background(), domain.Domain{
		Domain:            "example.com",
		OwnerUserID:       &userID,
		Status:            "active",
		Visibility:        "private",
		PublicationStatus: "draft",
		VerificationScore: 100,
		HealthStatus:      "healthy",
		Weight:            100,
	})
	if err != nil {
		t.Fatalf("seed root domain: %v", err)
	}

	verifiedSubdomain, err := domainRepo.Upsert(context.Background(), domain.Domain{
		Domain:            "mx.example.com",
		OwnerUserID:       &userID,
		Status:            "active",
		Visibility:        "private",
		PublicationStatus: "draft",
		VerificationScore: 100,
		HealthStatus:      "healthy",
		Weight:            90,
	})
	if err != nil {
		t.Fatalf("seed verified subdomain: %v", err)
	}

	_, err = domainRepo.Upsert(context.Background(), domain.Domain{
		Domain:            "relay.example.com",
		OwnerUserID:       &userID,
		Status:            "active",
		Visibility:        "private",
		PublicationStatus: "draft",
		VerificationScore: 0,
		HealthStatus:      "unknown",
		Weight:            90,
	})
	if err != nil {
		t.Fatalf("seed unverified subdomain: %v", err)
	}

	payload, err := service.BuildDashboard(context.Background(), userID)
	if err != nil {
		t.Fatalf("build dashboard: %v", err)
	}
	foundVerified := false
	foundRoot := false
	for _, item := range payload.AvailableDomains {
		if item.ID == verifiedSubdomain.ID {
			foundVerified = true
		}
		if item.Domain == "example.com" {
			foundRoot = true
		}
		if item.Domain == "relay.example.com" {
			t.Fatalf("expected unverified subdomain to be hidden from available domains")
		}
	}
	if !foundRoot {
		t.Fatalf("expected root domain to remain available")
	}
	if !foundVerified {
		t.Fatalf("expected verified subdomain to remain available")
	}
}

func TestCreatePermanentMailboxWithoutTTL(t *testing.T) {
	t.Parallel()

	domainRepo := domain.NewMemoryRepository(nil)
	mailboxRepo := NewMemoryRepository()
	service := NewService(mailboxRepo, domainRepo)
	userID := uint64(7)

	root, err := domainRepo.Upsert(context.Background(), domain.Domain{
		Domain:            "example.com",
		OwnerUserID:       &userID,
		Status:            "active",
		Visibility:        "private",
		PublicationStatus: "draft",
		VerificationScore: 100,
		HealthStatus:      "healthy",
		Weight:            100,
	})
	if err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	created, err := service.CreateMailbox(context.Background(), userID, CreateMailboxRequest{
		DomainID:    root.ID,
		LocalPart:   "forever",
		IsPermanent: true,
	})
	if err != nil {
		t.Fatalf("create permanent mailbox: %v", err)
	}
	if !created.IsPermanent {
		t.Fatalf("expected permanent mailbox, got %+v", created)
	}
	if !created.ExpiresAt.Equal(PermanentMailboxExpiresAt) {
		t.Fatalf("expected permanent expiresAt %v, got %v", PermanentMailboxExpiresAt, created.ExpiresAt)
	}

	listed, err := service.ListMailboxes(context.Background(), userID)
	if err != nil {
		t.Fatalf("list mailboxes: %v", err)
	}
	if len(listed) != 1 || !listed[0].IsPermanent {
		t.Fatalf("expected permanent mailbox in active list, got %+v", listed)
	}

	expiredIDs, err := mailboxRepo.ListExpiredIDs(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("list expired ids: %v", err)
	}
	if len(expiredIDs) != 0 {
		t.Fatalf("expected permanent mailbox to stay out of cleanup, got %v", expiredIDs)
	}
}

func TestMakeMailboxPermanent(t *testing.T) {
	t.Parallel()

	domainRepo := domain.NewMemoryRepository(nil)
	mailboxRepo := NewMemoryRepository()
	service := NewService(mailboxRepo, domainRepo)
	userID := uint64(7)

	root, err := domainRepo.Upsert(context.Background(), domain.Domain{
		Domain:            "example.com",
		OwnerUserID:       &userID,
		Status:            "active",
		Visibility:        "private",
		PublicationStatus: "draft",
		VerificationScore: 100,
		HealthStatus:      "healthy",
		Weight:            100,
	})
	if err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	created, err := service.CreateMailbox(context.Background(), userID, CreateMailboxRequest{
		DomainID:       root.ID,
		LocalPart:      "keeper",
		ExpiresInHours: 24,
	})
	if err != nil {
		t.Fatalf("create mailbox: %v", err)
	}
	if created.IsPermanent {
		t.Fatalf("expected temporary mailbox before conversion")
	}

	updated, err := service.MakeMailboxPermanent(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("make mailbox permanent: %v", err)
	}
	if !updated.IsPermanent {
		t.Fatalf("expected converted mailbox to be permanent, got %+v", updated)
	}
	if !updated.ExpiresAt.Equal(PermanentMailboxExpiresAt) {
		t.Fatalf("expected permanent expiresAt %v, got %v", PermanentMailboxExpiresAt, updated.ExpiresAt)
	}
	if updated.Status != "active" {
		t.Fatalf("expected converted mailbox to stay active, got %q", updated.Status)
	}

	expiredIDs, err := mailboxRepo.ListExpiredIDs(context.Background(), time.Now().Add(24*365*time.Hour))
	if err != nil {
		t.Fatalf("list expired ids: %v", err)
	}
	if len(expiredIDs) != 0 {
		t.Fatalf("expected converted mailbox to stay out of cleanup, got %v", expiredIDs)
	}
}

func pointerUint64(value uint64) *uint64 {
	return &value
}
