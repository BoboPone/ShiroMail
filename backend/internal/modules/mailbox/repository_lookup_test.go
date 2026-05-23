package mailbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryRepositoryFindActiveByAddress(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	active, err := repo.Create(ctx, Mailbox{
		UserID:    1,
		DomainID:  1,
		Domain:    "example.test",
		LocalPart: "alpha",
		Address:   "alpha@example.test",
		Status:    "active",
		ExpiresAt: time.Now().Add(2 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	found, err := repo.FindActiveByAddress(ctx, "alpha@example.test")
	if err != nil {
		t.Fatalf("find active mailbox: %v", err)
	}
	if found.ID != active.ID {
		t.Fatalf("expected mailbox %d, got %d", active.ID, found.ID)
	}
}

func TestMemoryRepositoryFindActiveByAddressRejectsExpiredMailbox(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	expired, err := repo.Create(ctx, Mailbox{
		UserID:    1,
		DomainID:  1,
		Domain:    "example.test",
		LocalPart: "expired",
		Address:   "expired@example.test",
		Status:    "active",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("create expired mailbox: %v", err)
	}

	_, err = repo.FindActiveByAddress(ctx, "expired@example.test")
	if !errors.Is(err, ErrMailboxNotFound) {
		t.Fatalf("expected ErrMailboxNotFound, got %v", err)
	}

	found, err := repo.FindByAddress(ctx, "expired@example.test")
	if err != nil {
		t.Fatalf("find expired mailbox by address: %v", err)
	}
	if found.ID != expired.ID {
		t.Fatalf("expected mailbox %d, got %d", expired.ID, found.ID)
	}
}
