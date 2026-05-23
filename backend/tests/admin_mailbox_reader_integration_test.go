package tests

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"shiro-email/backend/internal/bootstrap"
	"shiro-email/backend/internal/modules/ingest"
	"shiro-email/backend/internal/modules/mailbox"
)

func TestAdminMailboxOpenByAddressReadsPrivateDomainMessages(t *testing.T) {
	server, state := bootstrap.NewTestApp()
	adminToken := adminAccessToken(t)

	rr := performJSON(server, http.MethodPost, "/api/v1/admin/domains", `{"domain":"admin-reader-private.test","status":"active","visibility":"private","publicationStatus":"draft","healthStatus":"healthy","isDefault":false,"weight":100}`, adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected private domain creation success, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/admin/mailboxes/open", `{"address":"Reader@admin-reader-private.test"}`, adminToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected admin mailbox open success, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"address":"reader@admin-reader-private.test"`) {
		t.Fatalf("expected normalized mailbox address in response: %s", rr.Body.String())
	}
	mailboxID := extractJSONScalarField(rr.Body.String(), "id")
	if mailboxID == "" {
		t.Fatalf("expected mailbox id in open response: %s", rr.Body.String())
	}

	rawMessage := "From: sender@example.com\r\nTo: reader@admin-reader-private.test\r\nSubject: Admin private reader\r\n\r\nprivate mailbox body"
	if _, err := state.DirectIngest.Deliver(context.Background(), ingest.InboundEnvelope{
		MailFrom:   "sender@example.com",
		Recipients: []string{"reader@admin-reader-private.test"},
	}, strings.NewReader(rawMessage)); err != nil {
		t.Fatalf("expected direct ingest success, got %v", err)
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/admin/mailboxes/"+mailboxID+"/messages", "", adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected admin message list success, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"subject":"Admin private reader"`) {
		t.Fatalf("expected private message subject in admin list response: %s", rr.Body.String())
	}
	messageID := extractJSONScalarField(rr.Body.String(), "id")
	if messageID == "" {
		t.Fatalf("expected message id in admin list response: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/admin/mailboxes/"+mailboxID+"/messages/"+messageID, "", adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected admin message detail success, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"textBody":"private mailbox body"`) {
		t.Fatalf("expected private message body in admin detail response: %s", rr.Body.String())
	}
}

func TestAdminCreateMailboxRestoresCatchAllMessagesReceivedBeforeCreation(t *testing.T) {
	server, state := bootstrap.NewTestApp()
	adminToken := adminAccessToken(t)
	ctx := context.Background()

	rr := performJSON(server, http.MethodPost, "/api/v1/admin/domains", `{"domain":"admin-catchall-before-create.test","status":"active","visibility":"private","publicationStatus":"draft","healthStatus":"healthy","isDefault":false,"weight":100}`, adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected domain creation success, got %d: %s", rr.Code, rr.Body.String())
	}
	domainID := extractJSONScalarField(rr.Body.String(), "id")
	if domainID == "" {
		t.Fatalf("expected domain id in response: %s", rr.Body.String())
	}

	rawMessage := "From: sender@example.com\r\nTo: future@admin-catchall-before-create.test\r\nSubject: Before mailbox exists\r\n\r\npre-created mailbox body"
	if _, err := state.DirectIngest.Deliver(ctx, ingest.InboundEnvelope{
		MailFrom:   "sender@example.com",
		Recipients: []string{"future@admin-catchall-before-create.test"},
	}, strings.NewReader(rawMessage)); err != nil {
		t.Fatalf("expected catch-all ingest before mailbox creation, got %v", err)
	}

	orphaned, err := state.MessageRepo.ListByMailboxID(ctx, 0)
	if err != nil {
		t.Fatalf("expected orphan message lookup success, got %v", err)
	}
	if len(orphaned) != 1 || orphaned[0].MailboxAddress != "future@admin-catchall-before-create.test" {
		t.Fatalf("expected one orphan catch-all message, got %#v", orphaned)
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/admin/mailboxes", `{"userId":1,"domainId":`+domainID+`,"localPart":"future","expiresInHours":24}`, adminToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected mailbox creation success, got %d: %s", rr.Code, rr.Body.String())
	}
	mailboxID := extractJSONScalarField(rr.Body.String(), "id")
	if mailboxID == "" {
		t.Fatalf("expected mailbox id in response: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/admin/mailboxes/"+mailboxID+"/messages", "", adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected mailbox message list success, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"subject":"Before mailbox exists"`) {
		t.Fatalf("expected catch-all message subject after mailbox creation: %s", rr.Body.String())
	}
}

func TestAdminMailboxOpenByAddressAcceptsManagedSubdomains(t *testing.T) {
	server, state := bootstrap.NewTestApp()
	adminToken := adminAccessToken(t)

	rr := performJSON(server, http.MethodPost, "/api/v1/admin/domains", `{"domain":"admin-reader-parent.test","status":"active","visibility":"private","publicationStatus":"draft","healthStatus":"healthy","isDefault":false,"weight":100}`, adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected parent domain creation success, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/admin/mailboxes/open", `{"address":"Reader@mx.deep.admin-reader-parent.test"}`, adminToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected admin mailbox open success for managed subdomain, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"address":"reader@mx.deep.admin-reader-parent.test"`) {
		t.Fatalf("expected requested subdomain mailbox address in response: %s", rr.Body.String())
	}
	mailboxID := extractJSONScalarField(rr.Body.String(), "id")
	if mailboxID == "" {
		t.Fatalf("expected mailbox id in open response: %s", rr.Body.String())
	}

	rawMessage := "From: sender@example.com\r\nTo: reader@mx.deep.admin-reader-parent.test\r\nSubject: Admin subdomain reader\r\n\r\nsubdomain mailbox body"
	if _, err := state.DirectIngest.Deliver(context.Background(), ingest.InboundEnvelope{
		MailFrom:   "sender@example.com",
		Recipients: []string{"reader@mx.deep.admin-reader-parent.test"},
	}, strings.NewReader(rawMessage)); err != nil {
		t.Fatalf("expected direct ingest success for managed subdomain, got %v", err)
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/admin/mailboxes/"+mailboxID+"/messages", "", adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected admin subdomain message list success, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"subject":"Admin subdomain reader"`) {
		t.Fatalf("expected subdomain message subject in admin list response: %s", rr.Body.String())
	}
}

func TestAdminMailboxOpenByAddressReactivatesExpiredMailbox(t *testing.T) {
	server, state := bootstrap.NewTestApp()
	adminToken := adminAccessToken(t)
	ctx := context.Background()

	rr := performJSON(server, http.MethodPost, "/api/v1/admin/domains", `{"domain":"admin-reader-expired.test","status":"active","visibility":"private","publicationStatus":"draft","healthStatus":"healthy","isDefault":false,"weight":100}`, adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected domain creation success, got %d: %s", rr.Code, rr.Body.String())
	}

	domainID, err := strconv.ParseUint(extractJSONScalarField(rr.Body.String(), "id"), 10, 64)
	if err != nil || domainID == 0 {
		t.Fatalf("expected domain id in response, got %v: %s", err, rr.Body.String())
	}

	now := time.Now()
	expired, err := state.MailboxRepo.Create(ctx, mailbox.Mailbox{
		UserID:    1,
		DomainID:  domainID,
		Domain:    "admin-reader-expired.test",
		LocalPart: "reader",
		Address:   "reader@admin-reader-expired.test",
		Status:    "active",
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now.Add(-25 * time.Hour),
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("expected expired mailbox fixture, got %v", err)
	}

	historicalMessage := "From: sender@example.com\r\nTo: reader@admin-reader-expired.test\r\nSubject: Admin expired history\r\n\r\nexpired history body"
	if _, err := state.DirectIngest.Deliver(ctx, ingest.InboundEnvelope{
		MailFrom:   "sender@example.com",
		Recipients: []string{"reader@admin-reader-expired.test"},
	}, strings.NewReader(historicalMessage)); err != nil {
		t.Fatalf("expected historical ingest success, got %v", err)
	}
	if err := state.MessageRepo.SoftDeleteByMailboxIDs(ctx, []uint64{expired.ID}); err != nil {
		t.Fatalf("expected historical messages soft delete success, got %v", err)
	}
	expired.Status = "expired"
	expired.ExpiresAt = now.Add(-time.Hour)
	expired.UpdatedAt = now.Add(-time.Minute)
	if _, err := state.MailboxRepo.Update(ctx, expired); err != nil {
		t.Fatalf("expected expired mailbox update success, got %v", err)
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/admin/mailboxes/open", `{"address":"reader@admin-reader-expired.test"}`, adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected admin mailbox open to restore expired mailbox, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"id":`+strconv.FormatUint(expired.ID, 10)) {
		t.Fatalf("expected existing mailbox id in response: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"status":"active"`) {
		t.Fatalf("expected restored mailbox to be active: %s", rr.Body.String())
	}

	restored, err := state.MailboxRepo.FindByID(ctx, expired.ID)
	if err != nil {
		t.Fatalf("expected restored mailbox lookup success, got %v", err)
	}
	if !mailbox.IsActiveAt(restored, time.Now()) {
		t.Fatalf("expected restored mailbox to be active, got %+v", restored)
	}

	rawMessage := "From: sender@example.com\r\nTo: reader@admin-reader-expired.test\r\nSubject: Admin restored reader\r\n\r\nrestored mailbox body"
	if _, err := state.DirectIngest.Deliver(ctx, ingest.InboundEnvelope{
		MailFrom:   "sender@example.com",
		Recipients: []string{"reader@admin-reader-expired.test"},
	}, strings.NewReader(rawMessage)); err != nil {
		t.Fatalf("expected direct ingest success for restored mailbox, got %v", err)
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/admin/mailboxes/"+strconv.FormatUint(expired.ID, 10)+"/messages", "", adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected restored mailbox message list success, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"subject":"Admin restored reader"`) {
		t.Fatalf("expected restored mailbox message subject in response: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"subject":"Admin expired history"`) {
		t.Fatalf("expected restored historical message subject in response: %s", rr.Body.String())
	}
}

func TestAdminMailboxOpenByAddressRestoresReleasedMailboxMessages(t *testing.T) {
	server, state := bootstrap.NewTestApp()
	adminToken := adminAccessToken(t)
	ctx := context.Background()

	rr := performJSON(server, http.MethodPost, "/api/v1/admin/domains", `{"domain":"admin-reader-released.test","status":"active","visibility":"private","publicationStatus":"draft","healthStatus":"healthy","isDefault":false,"weight":100}`, adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected domain creation success, got %d: %s", rr.Code, rr.Body.String())
	}

	domainID, err := strconv.ParseUint(extractJSONScalarField(rr.Body.String(), "id"), 10, 64)
	if err != nil || domainID == 0 {
		t.Fatalf("expected domain id in response, got %v: %s", err, rr.Body.String())
	}

	now := time.Now()
	released, err := state.MailboxRepo.Create(ctx, mailbox.Mailbox{
		UserID:    1,
		DomainID:  domainID,
		Domain:    "admin-reader-released.test",
		LocalPart: "reader",
		Address:   "reader@admin-reader-released.test",
		Status:    "active",
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("expected released mailbox fixture, got %v", err)
	}

	historicalMessage := "From: sender@example.com\r\nTo: reader@admin-reader-released.test\r\nSubject: Admin released history\r\n\r\nreleased history body"
	if _, err := state.DirectIngest.Deliver(ctx, ingest.InboundEnvelope{
		MailFrom:   "sender@example.com",
		Recipients: []string{"reader@admin-reader-released.test"},
	}, strings.NewReader(historicalMessage)); err != nil {
		t.Fatalf("expected historical ingest success, got %v", err)
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/admin/mailboxes/"+strconv.FormatUint(released.ID, 10)+"/release", "", adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected release success, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/admin/mailboxes/open", `{"address":"reader@admin-reader-released.test"}`, adminToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected admin mailbox open to recreate released mailbox, got %d: %s", rr.Code, rr.Body.String())
	}
	newMailboxID := extractJSONScalarField(rr.Body.String(), "id")
	if newMailboxID == "" || newMailboxID == strconv.FormatUint(released.ID, 10) {
		t.Fatalf("expected recreated mailbox id in response: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"address":"reader@admin-reader-released.test"`) {
		t.Fatalf("expected recreated mailbox address in response: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/admin/mailboxes/"+newMailboxID+"/messages", "", adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected recreated mailbox message list success, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"subject":"Admin released history"`) {
		t.Fatalf("expected released historical message subject in response: %s", rr.Body.String())
	}
}

func TestAdminMailboxOpenByAddressRequiresAdmin(t *testing.T) {
	server, _ := bootstrap.NewTestApp()

	rr := performJSON(server, http.MethodPost, "/api/v1/admin/mailboxes/open", `{"address":"reader@shiro.local"}`, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without admin token, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/public/mailbox-domains", "", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected public mailbox domain endpoint to be removed, got %d: %s", rr.Code, rr.Body.String())
	}
}
