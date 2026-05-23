package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"shiro-email/backend/internal/bootstrap"
	"shiro-email/backend/internal/jobs"
	"shiro-email/backend/internal/modules/ingest"
	"shiro-email/backend/internal/modules/mailbox"
	"shiro-email/backend/internal/modules/message"
	"shiro-email/backend/internal/modules/system"
)

func TestListMailboxMessages(t *testing.T) {
	server, token, mailboxID, _ := newSeededMessageServer(t)

	rr := performJSON(server, http.MethodGet, "/api/v1/mailboxes/"+mailboxID+"/messages", "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"subject":"Seeded welcome"`) {
		t.Fatalf("expected seeded subject in response: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"textBody":"`) {
		t.Fatalf("expected list response to omit full message bodies: %s", rr.Body.String())
	}
}

func TestGetMailboxMessageDetail(t *testing.T) {
	server, token, mailboxID, messageID := newSeededMessageServer(t)

	rr := performJSON(server, http.MethodGet, "/api/v1/mailboxes/"+mailboxID+"/messages/"+messageID, "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"htmlPreview":"\u003cp\u003eHello seeded mailbox\u003c/p\u003e"`) {
		t.Fatalf("expected html preview in response: %s", rr.Body.String())
	}
}

func TestGetMailboxMessageRawSource(t *testing.T) {
	server, token, mailboxID, messageID, rawMessage, _ := newDirectInboundMessageServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mailboxes/"+mailboxID+"/messages/"+messageID+"/raw", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if contentType := rr.Header().Get("Content-Type"); !strings.Contains(contentType, "message/rfc822") {
		t.Fatalf("expected message/rfc822 content type, got %q", contentType)
	}
	if rr.Body.String() != rawMessage {
		t.Fatalf("expected raw message body, got %q", rr.Body.String())
	}
}

func TestGetMailboxMessageAttachment(t *testing.T) {
	server, token, mailboxID, messageID, _, attachmentBody := newDirectInboundMessageServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mailboxes/"+mailboxID+"/messages/"+messageID+"/attachments/0", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if contentType := rr.Header().Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		t.Fatalf("expected text/plain content type, got %q", contentType)
	}
	if rr.Body.String() != attachmentBody {
		t.Fatalf("expected attachment body %q, got %q", attachmentBody, rr.Body.String())
	}
}

func TestGetMailboxMessageParsedRawIncludesContentID(t *testing.T) {
	server, token, mailboxID, messageID, _, _ := newInlineCIDMessageServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mailboxes/"+mailboxID+"/messages/"+messageID+"/raw/parsed", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"contentId":"logo@test"`) {
		t.Fatalf("expected parsed raw attachment content id, got %s", rr.Body.String())
	}
}

func TestCleanupExpiredMailboxData(t *testing.T) {
	mailboxRepo := mailbox.NewMemoryRepository()
	messageRepo := message.NewMemoryRepository()

	expiredMailbox, err := mailboxRepo.Create(context.Background(), mailbox.Mailbox{
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
		t.Fatalf("expected expired mailbox fixture, got %v", err)
	}

	if err := messageRepo.UpsertFromLegacySync(context.Background(), expiredMailbox.ID, expiredMailbox.LocalPart, ingest.ParsedMessage{
		LegacyMailboxKey: expiredMailbox.LocalPart,
		LegacyMessageKey: "expired-1",
		FromAddr:         "cleanup@example.com",
		ToAddr:           expiredMailbox.Address,
		Subject:          "Cleanup me",
		TextPreview:      "cleanup-body",
		HTMLPreview:      "<p>cleanup-body</p>",
		ReceivedAt:       time.Now(),
	}); err != nil {
		t.Fatalf("expected seeded message, got %v", err)
	}

	if err := jobs.RunCleanupExpiredJob(context.Background(), mailboxRepo, messageRepo, nil, nil); err != nil {
		t.Fatalf("expected cleanup success, got %v", err)
	}

	msg, err := messageRepo.GetByMailboxAndID(context.Background(), expiredMailbox.ID, 1)
	if err != nil {
		t.Fatalf("expected stored message after cleanup, got %v", err)
	}
	if !msg.IsDeleted {
		t.Fatalf("expected message soft-deleted after cleanup: %+v", msg)
	}

	expiredIDs, err := mailboxRepo.ListExpiredIDs(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("expected expired mailbox ids, got %v", err)
	}
	if len(expiredIDs) != 0 {
		t.Fatalf("expected expired mailbox to be marked and removed from pending cleanup, got %v", expiredIDs)
	}
}

func TestCleanupExpiredMailboxDataSkipsPermanentMailboxes(t *testing.T) {
	mailboxRepo := mailbox.NewMemoryRepository()
	messageRepo := message.NewMemoryRepository()

	permanentMailbox, err := mailboxRepo.Create(context.Background(), mailbox.Mailbox{
		UserID:      1,
		DomainID:    1,
		Domain:      "example.test",
		LocalPart:   "permanent",
		Address:     "permanent@example.test",
		Status:      "active",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
		IsPermanent: true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("expected permanent mailbox fixture, got %v", err)
	}

	if err := messageRepo.UpsertFromLegacySync(context.Background(), permanentMailbox.ID, permanentMailbox.LocalPart, ingest.ParsedMessage{
		LegacyMailboxKey: permanentMailbox.LocalPart,
		LegacyMessageKey: "permanent-1",
		FromAddr:         "cleanup@example.com",
		ToAddr:           permanentMailbox.Address,
		Subject:          "Keep me",
		TextPreview:      "permanent-body",
		HTMLPreview:      "<p>permanent-body</p>",
		ReceivedAt:       time.Now(),
	}); err != nil {
		t.Fatalf("expected seeded message, got %v", err)
	}

	if err := jobs.RunCleanupExpiredJob(context.Background(), mailboxRepo, messageRepo, nil, nil); err != nil {
		t.Fatalf("expected cleanup success, got %v", err)
	}

	msg, err := messageRepo.GetByMailboxAndID(context.Background(), permanentMailbox.ID, 1)
	if err != nil {
		t.Fatalf("expected stored message after cleanup, got %v", err)
	}
	if msg.IsDeleted {
		t.Fatalf("expected permanent mailbox message to survive cleanup: %+v", msg)
	}
}

func TestCleanupExpiredMailboxDataRemovesExpiredStoredFiles(t *testing.T) {
	ctx := context.Background()
	mailboxRepo := mailbox.NewMemoryRepository()
	messageRepo := message.NewMemoryRepository()
	configRepo := system.NewMemoryConfigRepository()
	storageRoot := t.TempDir()
	storage, err := ingest.NewLocalFileStorage(storageRoot)
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}

	if _, err := configRepo.Upsert(ctx, system.ConfigKeyMailInboundPolicy, map[string]any{
		"retainRawDays":             1,
		"allowCatchAll":             false,
		"requireExistingMailbox":    true,
		"maxAttachmentSizeMB":       15,
		"rejectExecutableFiles":     true,
		"enableSpamScanningPreview": false,
	}, 0); err != nil {
		t.Fatalf("upsert inbound policy config: %v", err)
	}

	expiredMailbox, err := mailboxRepo.Create(ctx, mailbox.Mailbox{
		UserID:    1,
		DomainID:  1,
		Domain:    "example.test",
		LocalPart: "expired-cleanup",
		Address:   "expired-cleanup@example.test",
		Status:    "active",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("create expired mailbox: %v", err)
	}

	rawKey, err := storage.StoreRaw(ctx, expiredMailbox.Address, "cleanup-message", []byte("raw-body"))
	if err != nil {
		t.Fatalf("store raw: %v", err)
	}
	attachment, err := storage.StoreAttachment(ctx, expiredMailbox.Address, "cleanup-message", ingest.InboundAttachment{
		FileName:    "note.txt",
		ContentType: "text/plain",
		Content:     []byte("attachment-body"),
		SizeBytes:   int64(len("attachment-body")),
	}, 0)
	if err != nil {
		t.Fatalf("store attachment: %v", err)
	}

	if err := messageRepo.StoreInbound(ctx, expiredMailbox.ID, ingest.StoredInboundMessage{
		SourceKind:      "smtp",
		SourceMessageID: "cleanup-message",
		MailboxAddress:  expiredMailbox.Address,
		FromAddr:        "cleanup@example.com",
		ToAddr:          expiredMailbox.Address,
		Subject:         "Cleanup files",
		TextPreview:     "cleanup",
		TextBody:        "cleanup",
		RawStorageKey:   rawKey,
		HasAttachments:  true,
		SizeBytes:       128,
		ReceivedAt:      time.Now().Add(-48 * time.Hour),
		Attachments:     []ingest.StoredAttachment{attachment},
	}); err != nil {
		t.Fatalf("store inbound message: %v", err)
	}

	oldTime := time.Now().Add(-72 * time.Hour)
	rawPath := filepath.Join(storageRoot, filepath.FromSlash(rawKey))
	attachmentPath := filepath.Join(storageRoot, filepath.FromSlash(attachment.StorageKey))
	if err := os.Chtimes(rawPath, oldTime, oldTime); err != nil {
		t.Fatalf("age raw file: %v", err)
	}
	if err := os.Chtimes(attachmentPath, oldTime, oldTime); err != nil {
		t.Fatalf("age attachment file: %v", err)
	}

	if err := jobs.RunCleanupExpiredJob(ctx, mailboxRepo, messageRepo, configRepo, storage); err != nil {
		t.Fatalf("cleanup expired mailbox data: %v", err)
	}

	if _, err := os.Stat(rawPath); !os.IsNotExist(err) {
		t.Fatalf("expected expired raw file removed, got %v", err)
	}
	if _, err := os.Stat(attachmentPath); !os.IsNotExist(err) {
		t.Fatalf("expected expired attachment removed, got %v", err)
	}
}

func TestDirectIngestDeliversToMultipleUserMailboxes(t *testing.T) {
	server, state := bootstrap.NewTestApp()

	rr := performJSON(server, http.MethodPost, "/api/v1/auth/register", `{"username":"multi-direct","email":"multi-direct@example.com","password":"Secret123!"}`, "")
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 on register, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/auth/login", `{"login":"multi-direct","password":"Secret123!"}`, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on login, got %d: %s", rr.Code, rr.Body.String())
	}

	userIDText := extractJSONScalarField(rr.Body.String(), "userId")
	userID, err := strconv.ParseUint(userIDText, 10, 64)
	if err != nil {
		t.Fatalf("expected numeric user id in login response, got %q: %v", userIDText, err)
	}

	firstMailbox, err := state.MailboxRepo.Create(context.Background(), mailbox.Mailbox{
		UserID:    userID,
		DomainID:  1,
		Domain:    "example.test",
		LocalPart: "first-direct",
		Address:   "first-direct@example.test",
		Status:    "active",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("expected first mailbox create success, got %v", err)
	}
	secondMailbox, err := state.MailboxRepo.Create(context.Background(), mailbox.Mailbox{
		UserID:    userID,
		DomainID:  1,
		Domain:    "example.test",
		LocalPart: "second-direct",
		Address:   "second-direct@example.test",
		Status:    "active",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("expected second mailbox create success, got %v", err)
	}

	deliver := func(recipient string, subject string) {
		t.Helper()
		rawMessage := "From: sender@example.com\r\nTo: " + recipient + "\r\nSubject: " + subject + "\r\n\r\nhello direct mailbox"
		if _, deliverErr := state.DirectIngest.Deliver(context.Background(), ingest.InboundEnvelope{
			MailFrom:   "sender@example.com",
			Recipients: []string{recipient},
		}, strings.NewReader(rawMessage)); deliverErr != nil {
			t.Fatalf("expected direct ingest success for %s, got %v", recipient, deliverErr)
		}
	}

	deliver(firstMailbox.Address, "First mailbox")
	deliver(secondMailbox.Address, "Second mailbox")

	firstItems, err := state.MessageRepo.ListByMailboxID(context.Background(), firstMailbox.ID)
	if err != nil {
		t.Fatalf("expected first mailbox messages, got %v", err)
	}
	if len(firstItems) != 1 || firstItems[0].Subject != "First mailbox" {
		t.Fatalf("expected first mailbox message, got %#v", firstItems)
	}

	secondItems, err := state.MessageRepo.ListByMailboxID(context.Background(), secondMailbox.ID)
	if err != nil {
		t.Fatalf("expected second mailbox messages, got %v", err)
	}
	if len(secondItems) != 1 || secondItems[0].Subject != "Second mailbox" {
		t.Fatalf("expected second mailbox message, got %#v", secondItems)
	}
}

func newSeededMessageServer(t *testing.T) (http.Handler, string, string, string) {
	t.Helper()

	server, state := bootstrap.NewTestApp()

	registerBody := `{"username":"message-user","email":"message-user@example.com","password":"Secret123!"}`
	rr := performJSON(server, http.MethodPost, "/api/v1/auth/register", registerBody, "")
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 on register, got %d: %s", rr.Code, rr.Body.String())
	}

	loginBody := `{"login":"message-user","password":"Secret123!"}`
	rr = performJSON(server, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on login, got %d: %s", rr.Code, rr.Body.String())
	}

	token := extractJSONField(rr.Body.String(), "accessToken")
	if token == "" {
		t.Fatalf("expected access token in login response: %s", rr.Body.String())
	}
	userIDText := extractJSONScalarField(rr.Body.String(), "userId")
	userID, err := strconv.ParseUint(userIDText, 10, 64)
	if err != nil {
		t.Fatalf("expected numeric user id in login response, got %q: %v", userIDText, err)
	}

	seededMailbox, err := state.MailboxRepo.Create(context.Background(), mailbox.Mailbox{
		UserID:    userID,
		DomainID:  1,
		Domain:    "example.test",
		LocalPart: "seeded",
		Address:   "seeded@example.test",
		Status:    "active",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("expected seeded mailbox, got %v", err)
	}

	if err := state.MessageRepo.UpsertFromLegacySync(context.Background(), seededMailbox.ID, seededMailbox.LocalPart, ingest.ParsedMessage{
		LegacyMailboxKey: seededMailbox.LocalPart,
		LegacyMessageKey: "seed-1",
		FromAddr:         "hello@example.com",
		ToAddr:           seededMailbox.Address,
		Subject:          "Seeded welcome",
		TextPreview:      "Hello seeded mailbox",
		HTMLPreview:      "<p>Hello seeded mailbox</p>",
		ReceivedAt:       time.Now(),
		Attachments: []ingest.ParsedAttachment{
			{
				FileName:    "seed.txt",
				ContentType: "text/plain",
				StorageKey:  "seed.txt",
			},
		},
	}); err != nil {
		t.Fatalf("expected seeded message, got %v", err)
	}

	seededMessages, err := state.MessageRepo.ListByMailboxID(context.Background(), seededMailbox.ID)
	if err != nil {
		t.Fatalf("expected seeded messages, got %v", err)
	}
	if len(seededMessages) != 1 {
		t.Fatalf("expected exactly one seeded message, got %d", len(seededMessages))
	}

	return server, token, strconv.FormatUint(seededMailbox.ID, 10), strconv.FormatUint(seededMessages[0].ID, 10)
}

func newDirectInboundMessageServer(t *testing.T) (http.Handler, string, string, string, string, string) {
	t.Helper()

	server, state := bootstrap.NewTestApp()

	registerBody := `{"username":"direct-user","email":"direct-user@example.com","password":"Secret123!"}`
	rr := performJSON(server, http.MethodPost, "/api/v1/auth/register", registerBody, "")
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 on register, got %d: %s", rr.Code, rr.Body.String())
	}

	loginBody := `{"login":"direct-user","password":"Secret123!"}`
	rr = performJSON(server, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on login, got %d: %s", rr.Code, rr.Body.String())
	}

	token := extractJSONField(rr.Body.String(), "accessToken")
	if token == "" {
		t.Fatalf("expected access token in login response: %s", rr.Body.String())
	}
	userIDText := extractJSONScalarField(rr.Body.String(), "userId")
	userID, err := strconv.ParseUint(userIDText, 10, 64)
	if err != nil {
		t.Fatalf("expected numeric user id in login response, got %q: %v", userIDText, err)
	}

	targetMailbox, err := state.MailboxRepo.Create(context.Background(), mailbox.Mailbox{
		UserID:    userID,
		DomainID:  1,
		Domain:    "example.test",
		LocalPart: "direct",
		Address:   "direct@example.test",
		Status:    "active",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("expected seeded mailbox, got %v", err)
	}

	rawMessage := "From: sender@example.com\r\nTo: direct@example.test\r\nSubject: Direct welcome\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=abc\r\n\r\n--abc\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhello direct mailbox\r\n--abc\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=\"note.txt\"\r\n\r\nattachment body\r\n--abc--\r\n"
	if _, err := state.DirectIngest.Deliver(context.Background(), ingest.InboundEnvelope{
		MailFrom:   "sender@example.com",
		Recipients: []string{targetMailbox.Address},
	}, strings.NewReader(rawMessage)); err != nil {
		t.Fatalf("expected direct ingest success, got %v", err)
	}

	items, err := state.MessageRepo.ListByMailboxID(context.Background(), targetMailbox.ID)
	if err != nil {
		t.Fatalf("expected stored messages, got %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected exactly one stored message, got %d", len(items))
	}

	return server, token, strconv.FormatUint(targetMailbox.ID, 10), strconv.FormatUint(items[0].ID, 10), rawMessage, "attachment body"
}

func newInlineCIDMessageServer(t *testing.T) (http.Handler, string, string, string, string, string) {
	t.Helper()

	server, state := bootstrap.NewTestApp()

	registerBody := `{"username":"cid-user","email":"cid-user@example.com","password":"Secret123!"}`
	rr := performJSON(server, http.MethodPost, "/api/v1/auth/register", registerBody, "")
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 on register, got %d: %s", rr.Code, rr.Body.String())
	}

	loginBody := `{"login":"cid-user","password":"Secret123!"}`
	rr = performJSON(server, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on login, got %d: %s", rr.Code, rr.Body.String())
	}

	token := extractJSONField(rr.Body.String(), "accessToken")
	userIDText := extractJSONScalarField(rr.Body.String(), "userId")
	userID, err := strconv.ParseUint(userIDText, 10, 64)
	if err != nil {
		t.Fatalf("expected numeric user id in login response, got %q: %v", userIDText, err)
	}

	targetMailbox, err := state.MailboxRepo.Create(context.Background(), mailbox.Mailbox{
		UserID:    userID,
		DomainID:  1,
		Domain:    "example.test",
		LocalPart: "cid",
		Address:   "cid@example.test",
		Status:    "active",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("expected seeded mailbox, got %v", err)
	}

	rawMessage := "From: sender@example.com\r\nTo: cid@example.test\r\nSubject: Inline image\r\nMIME-Version: 1.0\r\nContent-Type: multipart/related; boundary=abc\r\n\r\n--abc\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<img src=\"cid:logo@test\">\r\n--abc\r\nContent-Type: image/png\r\nContent-Transfer-Encoding: base64\r\nContent-ID: <logo@test>\r\nContent-Disposition: inline; filename=\"logo.png\"\r\n\r\naGVsbG8=\r\n--abc--\r\n"
	if _, err := state.DirectIngest.Deliver(context.Background(), ingest.InboundEnvelope{
		MailFrom:   "sender@example.com",
		Recipients: []string{targetMailbox.Address},
	}, strings.NewReader(rawMessage)); err != nil {
		t.Fatalf("expected direct ingest success, got %v", err)
	}

	items, err := state.MessageRepo.ListByMailboxID(context.Background(), targetMailbox.ID)
	if err != nil {
		t.Fatalf("expected stored messages, got %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected exactly one stored message, got %d", len(items))
	}

	return server, token, strconv.FormatUint(targetMailbox.ID, 10), strconv.FormatUint(items[0].ID, 10), rawMessage, "hello"
}
