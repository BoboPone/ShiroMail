package mailbox

import (
	"time"

	"shiro-email/backend/internal/modules/domain"
)

type Mailbox struct {
	ID              uint64    `json:"id"`
	UserID          uint64    `json:"userId"`
	DomainID        uint64    `json:"domainId"`
	Domain          string    `json:"domain"`
	LocalPart       string    `json:"localPart"`
	Address         string    `json:"address"`
	Status          string    `json:"status"`
	Permanent       bool      `json:"permanent"`
	IsPermanent     bool      `json:"isPermanent"`
	ExpiresAt       time.Time `json:"expiresAt"`
	RetentionDays   int       `json:"retentionDays"`
	ForwardTo       string    `json:"forwardTo"`
	ForwardKeepCopy bool      `json:"forwardKeepCopy"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type CreateMailboxRequest struct {
	LocalPart      string `json:"localPart"`
	DomainID       uint64 `json:"domainId" binding:"required"`
	ExpiresInHours int    `json:"expiresInHours"`
	Permanent      bool   `json:"permanent"`
	RetentionDays  int    `json:"retentionDays"`
	IsPermanent    bool   `json:"isPermanent"`
}

type OpenMailboxByAddressRequest struct {
	Address string `json:"address" binding:"required"`
}

type ExtendMailboxRequest struct {
	ExpiresInHours int `json:"expiresInHours" binding:"required"`
}

type UpdateForwardingRequest struct {
	ForwardTo       string `json:"forwardTo"`
	ForwardKeepCopy bool   `json:"forwardKeepCopy"`
}

type DashboardPayload struct {
	TotalMailboxCount  int             `json:"totalMailboxCount"`
	ActiveMailboxCount int             `json:"activeMailboxCount"`
	AvailableDomains   []domain.Domain `json:"availableDomains"`
	Mailboxes          []Mailbox       `json:"mailboxes"`
	UnreadCounts       map[uint64]int  `json:"unreadCounts"`
}

var PermanentMailboxExpiresAt = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

func ResolveExpiresAt(req CreateMailboxRequest, now time.Time) time.Time {
	if req.IsPermanent || req.Permanent {
		return PermanentMailboxExpiresAt
	}
	return now.Add(time.Duration(req.ExpiresInHours) * time.Hour)
}

func IsActiveAt(item Mailbox, now time.Time) bool {
	return item.Status == "active" && (item.IsPermanent || item.Permanent || item.ExpiresAt.After(now))
}
