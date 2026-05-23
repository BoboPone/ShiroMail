package tests

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"shiro-email/backend/internal/bootstrap"
	"shiro-email/backend/internal/config"
	"shiro-email/backend/internal/shared/security"
)

func newAuthedServer(t *testing.T, username string) (http.Handler, string) {
	t.Helper()

	server := newTestServer(t)
	registerBody := `{"username":"` + username + `","email":"` + username + `@example.com","password":"Secret123!"}`
	rr := performJSON(server, http.MethodPost, "/api/v1/auth/register", registerBody, "")
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 on register, got %d: %s", rr.Code, rr.Body.String())
	}

	loginBody := `{"login":"` + username + `","password":"Secret123!"}`
	rr = performJSON(server, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on login, got %d: %s", rr.Code, rr.Body.String())
	}

	accessToken := extractJSONField(rr.Body.String(), "accessToken")
	if accessToken == "" {
		t.Fatalf("expected access token in login response: %s", rr.Body.String())
	}

	return server, accessToken
}

func registerAndLogin(t *testing.T, server http.Handler, username string) string {
	t.Helper()

	registerBody := `{"username":"` + username + `","email":"` + username + `@example.com","password":"Secret123!"}`
	rr := performJSON(server, http.MethodPost, "/api/v1/auth/register", registerBody, "")
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 on register, got %d: %s", rr.Code, rr.Body.String())
	}

	loginBody := `{"login":"` + username + `","password":"Secret123!"}`
	rr = performJSON(server, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on login, got %d: %s", rr.Code, rr.Body.String())
	}

	accessToken := extractJSONField(rr.Body.String(), "accessToken")
	if accessToken == "" {
		t.Fatalf("expected access token in login response: %s", rr.Body.String())
	}

	return accessToken
}

func adminAccessToken(t *testing.T) string {
	t.Helper()

	token, err := security.SignAccessToken(2, []string{"admin"}, config.MustLoadConfig().JWTSecret)
	if err != nil {
		t.Fatalf("expected admin access token, got %v", err)
	}
	return token
}

func TestCreateMailboxFlow(t *testing.T) {
	server, token := newAuthedServer(t, "mailbox-user")

	rr := performJSON(server, http.MethodPost, "/api/v1/mailboxes", `{"domainId":1,"expiresInHours":24}`, token)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"address":"`) {
		t.Fatalf("expected mailbox address in response: %s", rr.Body.String())
	}
}

func TestCreatePermanentMailboxFlow(t *testing.T) {
	server, token := newAuthedServer(t, "mailbox-permanent-user")

	rr := performJSON(server, http.MethodPost, "/api/v1/mailboxes", `{"domainId":1,"isPermanent":true}`, token)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"isPermanent":true`) {
		t.Fatalf("expected permanent mailbox response: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/dashboard", "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on dashboard, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"activeMailboxCount":1`) || !strings.Contains(rr.Body.String(), `"isPermanent":true`) {
		t.Fatalf("expected permanent mailbox in dashboard: %s", rr.Body.String())
	}
}

func TestMakeExistingMailboxPermanentFlow(t *testing.T) {
	server, token := newAuthedServer(t, "mailbox-convert-permanent-user")

	rr := performJSON(server, http.MethodPost, "/api/v1/mailboxes", `{"domainId":1,"expiresInHours":24}`, token)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"isPermanent":true`) {
		t.Fatalf("expected temporary mailbox before conversion: %s", rr.Body.String())
	}
	mailboxID := extractJSONScalarField(rr.Body.String(), "id")
	if mailboxID == "" {
		t.Fatalf("expected mailbox id in response: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/mailboxes/"+mailboxID+"/permanent", `{}`, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on make permanent, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"isPermanent":true`) || !strings.Contains(rr.Body.String(), `"status":"active"`) {
		t.Fatalf("expected converted permanent mailbox response: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/dashboard", "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on dashboard, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"isPermanent":true`) || !strings.Contains(rr.Body.String(), `"activeMailboxCount":1`) {
		t.Fatalf("expected converted permanent mailbox in dashboard: %s", rr.Body.String())
	}
}

func TestExtendAndReleaseMailboxFlow(t *testing.T) {
	server, token := newAuthedServer(t, "mailbox-operator")

	rr := performJSON(server, http.MethodPost, "/api/v1/mailboxes", `{"domainId":1,"expiresInHours":24}`, token)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create, got %d: %s", rr.Code, rr.Body.String())
	}

	mailboxID := extractJSONScalarField(rr.Body.String(), "id")
	if mailboxID == "" {
		t.Fatalf("expected mailbox id in response: %s", rr.Body.String())
	}

	rawMessage := "From: sender@example.com\r\nTo: release-target@shiro.local\r\nSubject: Release Target\r\n\r\nbody"
	rr = performJSON(server, http.MethodPost, "/api/v1/mailboxes/"+mailboxID+"/messages/receive", `{"mailFrom":"sender@example.com","raw":`+strconv.Quote(rawMessage)+`}`, token)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 on receive before release, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/mailboxes/"+mailboxID+"/extend", `{"expiresInHours":72}`, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on extend, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"status":"active"`) {
		t.Fatalf("expected active mailbox after extend: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/mailboxes/"+mailboxID+"/release", `{}`, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on release, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"status":"released"`) {
		t.Fatalf("expected released mailbox after release: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/dashboard", "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on dashboard after release, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"address":"alpha@`) {
		t.Fatalf("expected released mailbox to disappear from user dashboard: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"activeMailboxCount":0`) {
		t.Fatalf("expected active mailbox count to drop to 0 after release: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/mailboxes", "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on mailbox list after release, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"id":`+mailboxID) {
		t.Fatalf("expected released mailbox to disappear from mailbox list: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/mailboxes/"+mailboxID+"/messages", "", token)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on released mailbox messages, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDashboardSummaryFlow(t *testing.T) {
	server, token := newAuthedServer(t, "dashboard-user")

	rr := performJSON(server, http.MethodGet, "/api/v1/dashboard", "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on dashboard, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"availableDomains":`) {
		t.Fatalf("expected available domains in dashboard response: %s", rr.Body.String())
	}
}

func TestDomainListingsOnlyExposeOwnedAndPublicDomains(t *testing.T) {
	server, _ := bootstrap.NewTestApp()
	adminToken := adminAccessToken(t)
	userToken := registerAndLogin(t, server, "domain-user")
	otherUserToken := registerAndLogin(t, server, "other-domain-user")

	rr := performJSON(server, http.MethodPost, "/api/v1/domains", `{"domain":"owned-private.test","status":"active"}`, userToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected owned domain create to succeed, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/domains", `{"domain":"other-private.test","status":"active"}`, otherUserToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected other owned domain create to succeed, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/admin/domains", `{"domain":"shared-public-pool.test","status":"active","visibility":"public_pool","publicationStatus":"published"}`, adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected public pool domain create to succeed, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/admin/domains", `{"domain":"platform-public.test","status":"active","visibility":"platform_public","publicationStatus":"published"}`, adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected platform public domain create to succeed, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/domains", "", userToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on domain list, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"domain":"owned-private.test"`) {
		t.Fatalf("expected own private domain in domain list: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"domain":"other-private.test"`) {
		t.Fatalf("expected other user's private domain to be hidden: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"domain":"shared-public-pool.test"`) {
		t.Fatalf("expected shared public pool domain in domain list: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"domain":"platform-public.test"`) {
		t.Fatalf("expected platform public domain in domain list: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/dashboard", "", userToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on dashboard, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"domain":"owned-private.test"`) {
		t.Fatalf("expected own private domain in dashboard: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"domain":"other-private.test"`) {
		t.Fatalf("expected other user's private domain to be hidden from dashboard: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"domain":"shared-public-pool.test"`) {
		t.Fatalf("expected shared public pool domain in dashboard: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"domain":"platform-public.test"`) {
		t.Fatalf("expected platform public domain in dashboard: %s", rr.Body.String())
	}
}

func TestCreateMailboxRejectsAnotherUsersPrivateDomain(t *testing.T) {
	server, _ := bootstrap.NewTestApp()
	userToken := registerAndLogin(t, server, "mailbox-owner")
	otherUserToken := registerAndLogin(t, server, "mailbox-other-owner")

	rr := performJSON(server, http.MethodPost, "/api/v1/domains", `{"domain":"owned-mailbox.test","status":"active"}`, userToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected own domain create to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	ownedDomainID := extractJSONScalarField(rr.Body.String(), "id")
	if ownedDomainID == "" {
		t.Fatalf("expected own domain id in response: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/domains", `{"domain":"other-owned-mailbox.test","status":"active"}`, otherUserToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected other domain create to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	otherDomainID := extractJSONScalarField(rr.Body.String(), "id")
	if otherDomainID == "" {
		t.Fatalf("expected other domain id in response: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/mailboxes", `{"domainId":`+ownedDomainID+`,"expiresInHours":24}`, userToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected mailbox create on owned domain to succeed, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/mailboxes", `{"domainId":`+otherDomainID+`,"expiresInHours":24}`, userToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when creating mailbox on another user's private domain, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPublicPoolPublicationWorkflowRequiresApproval(t *testing.T) {
	server, _ := bootstrap.NewTestApp()
	adminToken := adminAccessToken(t)
	ownerToken := registerAndLogin(t, server, "publish-owner")
	otherUserToken := registerAndLogin(t, server, "publish-consumer")

	rr := performJSON(server, http.MethodPost, "/api/v1/domains", `{"domain":"publish-me.test","status":"active"}`, ownerToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected domain create to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	domainID := extractJSONScalarField(rr.Body.String(), "id")
	if domainID == "" {
		t.Fatalf("expected domain id in response: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/domains/"+domainID+"/public-pool", `{}`, ownerToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected publication request to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"publicationStatus":"pending_review"`) {
		t.Fatalf("expected pending review status after publication request: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/domains", "", otherUserToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on consumer domain list, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"domain":"publish-me.test"`) {
		t.Fatalf("expected pending-review domain to stay hidden from other users: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/admin/domains/"+domainID+"/public-pool/review", `{"decision":"approve"}`, adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected admin approval to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"publicationStatus":"approved"`) {
		t.Fatalf("expected approved status after admin approval: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/domains", "", otherUserToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on consumer domain list after approval, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"domain":"publish-me.test"`) {
		t.Fatalf("expected approved public-pool domain to be visible to other users: %s", rr.Body.String())
	}
}

func TestPublicPoolPublicationCanAutoApproveWhenReviewDisabled(t *testing.T) {
	server, _ := bootstrap.NewTestApp()
	adminToken := adminAccessToken(t)
	ownerToken := registerAndLogin(t, server, "auto-publish-owner")
	otherUserToken := registerAndLogin(t, server, "auto-publish-consumer")

	rr := performJSON(server, http.MethodPut, "/api/v1/admin/configs/domain.public_pool_policy", `{"value":{"requiresReview":false}}`, adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected config update to succeed, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/domains", `{"domain":"auto-approve.test","status":"active"}`, ownerToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected domain create to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	domainID := extractJSONScalarField(rr.Body.String(), "id")
	if domainID == "" {
		t.Fatalf("expected domain id in response: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/domains/"+domainID+"/public-pool", `{}`, ownerToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected publication request to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"publicationStatus":"approved"`) {
		t.Fatalf("expected immediate approval when review is disabled: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/domains", "", otherUserToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on consumer domain list, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"domain":"auto-approve.test"`) {
		t.Fatalf("expected auto-approved public-pool domain to be visible to other users: %s", rr.Body.String())
	}
}

func TestOwnerCanWithdrawPublicPoolDomain(t *testing.T) {
	server, _ := bootstrap.NewTestApp()
	adminToken := adminAccessToken(t)
	ownerToken := registerAndLogin(t, server, "withdraw-owner")
	otherUserToken := registerAndLogin(t, server, "withdraw-consumer")

	rr := performJSON(server, http.MethodPut, "/api/v1/admin/configs/domain.public_pool_policy", `{"value":{"requiresReview":false}}`, adminToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected config update to succeed, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/domains", `{"domain":"withdraw-me.test","status":"active"}`, ownerToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected domain create to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	domainID := extractJSONScalarField(rr.Body.String(), "id")
	if domainID == "" {
		t.Fatalf("expected domain id in response: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/domains/"+domainID+"/public-pool", `{}`, ownerToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected publication request to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"publicationStatus":"approved"`) {
		t.Fatalf("expected approved public-pool state before withdraw: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/domains", "", otherUserToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on consumer domain list before withdraw, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"domain":"withdraw-me.test"`) {
		t.Fatalf("expected public-pool domain visible before withdraw: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodPost, "/api/v1/domains/"+domainID+"/public-pool/withdraw", `{}`, ownerToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected withdraw to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"visibility":"private"`) || !strings.Contains(rr.Body.String(), `"publicationStatus":"draft"`) {
		t.Fatalf("expected withdraw to restore private draft state: %s", rr.Body.String())
	}

	rr = performJSON(server, http.MethodGet, "/api/v1/domains", "", otherUserToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on consumer domain list after withdraw, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"domain":"withdraw-me.test"`) {
		t.Fatalf("expected withdrawn domain to be hidden from other users: %s", rr.Body.String())
	}
}

func extractJSONScalarField(body string, key string) string {
	if value := extractJSONField(body, key); value != "" {
		return value
	}

	needle := `"` + key + `":`
	start := strings.Index(body, needle)
	if start == -1 {
		return ""
	}
	start += len(needle)
	end := start
	for end < len(body) {
		ch := body[end]
		if ch < '0' || ch > '9' {
			break
		}
		end++
	}
	return body[start:end]
}
