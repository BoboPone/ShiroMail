package tests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	_ "github.com/go-sql-driver/mysql"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"shiro-email/backend/internal/bootstrap"
	"shiro-email/backend/internal/config"
	"shiro-email/backend/internal/database"
	"shiro-email/backend/internal/modules/auth"
	"shiro-email/backend/internal/modules/domain"
	sharedcache "shiro-email/backend/internal/shared/cache"
	"shiro-email/backend/internal/shared/security"
)

func TestSchemaBootstrapCreatesRulesTable(t *testing.T) {
	db := mustOpenTestMySQL(t)

	mustResetPersistenceTables(t, db)

	mustEnsureSchema(t, db)

	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'rules'").Scan(&count).Error; err != nil {
		t.Fatalf("expected table lookup success, got %v", err)
	}
	if count != 1 {
		t.Fatalf("expected rules table to exist, got %d", count)
	}
}

func TestSchemaBootstrapMigratesLegacyMessageColumnsFromOldLayout(t *testing.T) {
	db := mustOpenTestMySQL(t)

	mustResetPersistenceTables(t, db)

	legacyTableSQL := `
CREATE TABLE messages (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  mailbox_id BIGINT NOT NULL,
  inbucket_mailbox VARCHAR(255) NOT NULL,
  inbucket_message_id VARCHAR(255) NOT NULL,
  source_kind VARCHAR(32) NOT NULL DEFAULT 'inbucket',
  source_message_id VARCHAR(255) NOT NULL DEFAULT '',
  mailbox_address VARCHAR(320) NOT NULL DEFAULT '',
  from_addr VARCHAR(320) NOT NULL,
  to_addr VARCHAR(320) NOT NULL,
  subject VARCHAR(512) NOT NULL,
  text_preview TEXT NULL,
  html_preview MEDIUMTEXT NULL,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  is_read BOOLEAN NOT NULL DEFAULT FALSE,
  is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
  received_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_inbucket_message (inbucket_mailbox, inbucket_message_id)
)`
	if err := db.Exec(legacyTableSQL).Error; err != nil {
		t.Fatalf("expected old-layout messages table creation, got %v", err)
	}

	insert := db.Exec(
		"INSERT INTO messages (mailbox_id, inbucket_mailbox, inbucket_message_id, source_kind, source_message_id, mailbox_address, from_addr, to_addr, subject, text_preview, html_preview, size_bytes, is_read, is_deleted, received_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		1,
		"alpha@old-layout.test",
		"legacy-001",
		"inbucket",
		"",
		"alpha@old-layout.test",
		"sender@example.com",
		"alpha@old-layout.test",
		"Old layout subject",
		"hello",
		"<p>hello</p>",
		64,
		false,
		false,
		time.Date(2026, 4, 2, 9, 30, 0, 0, time.UTC),
	)
	if insert.Error != nil {
		t.Fatalf("expected old-layout message insert success, got %v", insert.Error)
	}

	mustEnsureSchema(t, db)

	var legacyMailboxColumnCount int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'messages' AND column_name = 'legacy_mailbox_key'").Scan(&legacyMailboxColumnCount).Error; err != nil {
		t.Fatalf("expected legacy_mailbox_key column lookup success, got %v", err)
	}
	if legacyMailboxColumnCount != 1 {
		t.Fatalf("expected legacy_mailbox_key column to exist, got %d", legacyMailboxColumnCount)
	}

	var legacyMessageColumnCount int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'messages' AND column_name = 'legacy_message_key'").Scan(&legacyMessageColumnCount).Error; err != nil {
		t.Fatalf("expected legacy_message_key column lookup success, got %v", err)
	}
	if legacyMessageColumnCount != 1 {
		t.Fatalf("expected legacy_message_key column to exist, got %d", legacyMessageColumnCount)
	}

	var compatMailboxColumnCount int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'messages' AND column_name = 'inbucket_mailbox'").Scan(&compatMailboxColumnCount).Error; err != nil {
		t.Fatalf("expected inbucket_mailbox compatibility column lookup success, got %v", err)
	}
	if compatMailboxColumnCount != 0 {
		t.Fatalf("expected inbucket_mailbox compatibility column to be dropped, got %d", compatMailboxColumnCount)
	}

	var compatMessageColumnCount int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'messages' AND column_name = 'inbucket_message_id'").Scan(&compatMessageColumnCount).Error; err != nil {
		t.Fatalf("expected inbucket_message_id compatibility column lookup success, got %v", err)
	}
	if compatMessageColumnCount != 0 {
		t.Fatalf("expected inbucket_message_id compatibility column to be dropped, got %d", compatMessageColumnCount)
	}

	var migrated struct {
		LegacyMailboxKey string
		LegacyMessageKey string
		SourceKind       string
	}
	if err := db.Raw("SELECT legacy_mailbox_key, legacy_message_key, source_kind FROM messages WHERE id = 1").Scan(&migrated).Error; err != nil {
		t.Fatalf("expected migrated message lookup success, got %v", err)
	}
	if migrated.LegacyMailboxKey != "alpha@old-layout.test" {
		t.Fatalf("expected migrated legacy mailbox key, got %q", migrated.LegacyMailboxKey)
	}
	if migrated.LegacyMessageKey != "legacy-001" {
		t.Fatalf("expected migrated legacy message key, got %q", migrated.LegacyMessageKey)
	}
	if migrated.SourceKind != "legacy-sync" {
		t.Fatalf("expected migrated source kind legacy-sync, got %q", migrated.SourceKind)
	}
}

func TestRuntimeBootstrapSeedsPersistedDataIdempotently(t *testing.T) {
	db := mustOpenTestMySQL(t)
	redisClient := mustOpenTestRedis(t)

	mustResetPersistenceTables(t, db)
	mustFlushRedis(t, redisClient)

	cfg := config.Config{
		AppPort:               "8080",
		MySQLDSN:              testMySQLDSN(t),
		RedisAddr:             testRedisAddr(),
		JWTSecret:             "dev-secret",
		LegacyMailSyncAPIURL:  "http://127.0.0.1:9000",
		LegacyMailSyncEnabled: false,
	}

	if _, err := bootstrap.NewRuntimeAppForTest(cfg); err != nil {
		t.Fatalf("expected runtime app creation success, got %v", err)
	}
	if _, err := bootstrap.NewRuntimeAppForTest(cfg); err != nil {
		t.Fatalf("expected second runtime app creation success, got %v", err)
	}

	var userCount int64
	if err := db.Raw("SELECT COUNT(*) FROM users").Scan(&userCount).Error; err != nil {
		t.Fatalf("expected user count query success, got %v", err)
	}
	if userCount != 0 {
		t.Fatalf("expected runtime bootstrap to avoid demo users, got %d", userCount)
	}
}

func TestDashboardCacheInvalidatesAfterMailboxCreate(t *testing.T) {
	db := mustOpenTestMySQL(t)
	redisClient := mustOpenTestRedis(t)
	ctx := context.Background()

	mustResetPersistenceTables(t, db)
	mustFlushRedis(t, redisClient)

	cfg := runtimeTestConfig(t)
	server, err := bootstrap.NewRuntimeAppForTest(cfg)
	if err != nil {
		t.Fatalf("expected runtime app creation success, got %v", err)
	}

	userID := mustSeedLoginableUser(t, db, "cache-user", "cache-user@example.com", "Secret123!", []string{"user"})
	domainID := mustSeedOwnedDomain(t, db, userID, "cache-owned.test")

	cache := sharedcache.NewJSONCache(redisClient)

	loginBody := `{"login":"cache-user","password":"Secret123!"}`
	rr := performJSON(server, "POST", "/api/v1/auth/login", loginBody, "")
	if rr.Code != 200 {
		t.Fatalf("expected login success, got %d: %s", rr.Code, rr.Body.String())
	}
	accessToken := extractJSONField(rr.Body.String(), "accessToken")
	if accessToken == "" {
		t.Fatalf("expected access token in response: %s", rr.Body.String())
	}
	userIDText := extractJSONScalarField(rr.Body.String(), "userId")
	if userIDText == "" {
		t.Fatalf("expected user id in response: %s", rr.Body.String())
	}

	if err := cache.Set(ctx, "cache:dashboard:user:"+userIDText, time.Minute, map[string]any{"totalMailboxCount": 1}); err != nil {
		t.Fatalf("expected dashboard cache set success, got %v", err)
	}
	if err := cache.Set(ctx, "cache:admin:overview", time.Minute, map[string]any{"activeMailboxCount": 1}); err != nil {
		t.Fatalf("expected admin overview cache set success, got %v", err)
	}

	rr = performJSON(server, "POST", "/api/v1/mailboxes", fmt.Sprintf(`{"domainId":%d,"expiresInHours":24}`, domainID), accessToken)
	if rr.Code != 201 {
		t.Fatalf("expected mailbox create success, got %d: %s", rr.Code, rr.Body.String())
	}

	if _, err := redisClient.Get(ctx, "cache:dashboard:user:"+userIDText).Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("expected dashboard cache to be deleted, got %v", err)
	}
	if _, err := redisClient.Get(ctx, "cache:admin:overview").Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("expected admin overview cache to be deleted, got %v", err)
	}
}

func TestMySQLRepositoryUpsertTOTPCredentialOverwritesZeroValues(t *testing.T) {
	db := mustOpenTestMySQL(t)
	mustResetPersistenceTables(t, db)
	mustEnsureSchema(t, db)

	userID := mustSeedUser(t, db, "totp-user", "totp-user@example.com", []string{"user"})
	repo := auth.NewMySQLRepository(db)
	ctx := context.Background()

	if err := repo.UpsertTOTPCredential(ctx, auth.TOTPCredential{
		UserID:           userID,
		SecretCiphertext: "SECRET123",
		Enabled:          true,
	}); err != nil {
		t.Fatalf("expected initial totp upsert success, got %v", err)
	}

	if err := repo.UpsertTOTPCredential(ctx, auth.TOTPCredential{
		UserID:           userID,
		SecretCiphertext: "",
		Enabled:          false,
		VerifiedAt:       nil,
		LastUsedAt:       nil,
	}); err != nil {
		t.Fatalf("expected disabling totp upsert success, got %v", err)
	}

	credential, err := repo.FindTOTPCredentialByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("expected totp credential lookup success, got %v", err)
	}
	if credential.Enabled {
		t.Fatalf("expected totp to be disabled, got %+v", credential)
	}
	if credential.SecretCiphertext != "" {
		t.Fatalf("expected secret to be cleared, got %+v", credential)
	}
}

func mustOpenTestMySQL(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := testMySQLDSN(t)
	mustEnsureTestDatabase(t, dsn)

	db, err := database.NewMySQL(dsn)
	if err != nil {
		t.Fatalf("expected mysql connection, got %v", err)
	}
	return db
}

func mustEnsureSchema(t *testing.T, db *gorm.DB) {
	t.Helper()

	if err := database.EnsureSchema(context.Background(), db); err != nil {
		t.Fatalf("expected schema bootstrap success, got %v", err)
	}
}

func mustOpenTestRedis(t *testing.T) *redis.Client {
	t.Helper()

	addr := testRedisAddr()

	client := database.NewRedis(addr, "")
	if err := waitForRedis(context.Background(), client, 3*time.Second); err != nil {
		t.Skipf("skipping redis-backed persistence test: %v", err)
	}
	return client
}

func mustFlushRedis(t *testing.T, client *redis.Client) {
	t.Helper()

	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("expected redis flush success, got %v", err)
	}
}

func mustSeedRoles(t *testing.T, db *gorm.DB, roles ...string) {
	t.Helper()

	for _, role := range roles {
		result := db.Exec(
			"INSERT INTO roles (code, name) VALUES (?, ?) ON DUPLICATE KEY UPDATE name = VALUES(name)",
			role,
			role,
		)
		if result.Error != nil {
			t.Fatalf("expected role seed success for %s, got %v", role, result.Error)
		}
	}
}

func mustSeedUser(t *testing.T, db *gorm.DB, username string, email string, roles []string) uint64 {
	t.Helper()

	mustSeedRoles(t, db, roles...)

	repo := auth.NewMySQLRepository(db)
	user, err := repo.CreateUser(context.Background(), auth.User{
		Username:     username,
		Email:        email,
		PasswordHash: "seed-hash",
		Roles:        roles,
	})
	if err != nil {
		t.Fatalf("expected user seed success for %s, got %v", username, err)
	}
	return user.ID
}

func mustSeedLoginableUser(t *testing.T, db *gorm.DB, username string, email string, password string, roles []string) uint64 {
	t.Helper()

	mustSeedRoles(t, db, roles...)

	passwordHash, err := security.HashPassword(password)
	if err != nil {
		t.Fatalf("expected password hash success for %s, got %v", username, err)
	}

	repo := auth.NewMySQLRepository(db)
	user, err := repo.CreateUser(context.Background(), auth.User{
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		Roles:        roles,
	})
	if err != nil {
		t.Fatalf("expected loginable user seed success for %s, got %v", username, err)
	}
	return user.ID
}

func mustSeedDomain(t *testing.T, db *gorm.DB, domainName string) uint64 {
	t.Helper()

	result := db.Exec(
		"INSERT INTO domains (domain, status, is_default, weight, daily_limit) VALUES (?, 'active', FALSE, 100, 0)",
		domainName,
	)
	if result.Error != nil {
		t.Fatalf("expected domain seed success for %s, got %v", domainName, result.Error)
	}

	var domainID uint64
	if err := db.Raw("SELECT id FROM domains WHERE domain = ?", domainName).Scan(&domainID).Error; err != nil {
		t.Fatalf("expected domain id lookup success for %s, got %v", domainName, err)
	}
	return domainID
}

func mustSeedOwnedDomain(t *testing.T, db *gorm.DB, ownerUserID uint64, domainName string) uint64 {
	t.Helper()

	repo := domain.NewMySQLRepository(db)
	item, err := repo.Upsert(context.Background(), domain.Domain{
		Domain:            domainName,
		Status:            "active",
		OwnerUserID:       &ownerUserID,
		Visibility:        "private",
		PublicationStatus: "draft",
		HealthStatus:      "healthy",
		Weight:            100,
	})
	if err != nil {
		t.Fatalf("expected owned domain seed success for %s, got %v", domainName, err)
	}
	return item.ID
}

func mustSeedMailbox(t *testing.T, db *gorm.DB, userID uint64, domainID uint64, localPart string) uint64 {
	t.Helper()

	var domainName string
	if err := db.Raw("SELECT domain FROM domains WHERE id = ?", domainID).Scan(&domainName).Error; err != nil {
		t.Fatalf("expected domain lookup success for mailbox seed %s, got %v", localPart, err)
	}
	address := localPart + "@" + domainName
	result := db.Exec(
		"INSERT INTO mailboxes (user_id, domain_id, local_part, address, status, expires_at, is_favorite, source, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', ?, FALSE, 'manual', NOW(), NOW())",
		userID,
		domainID,
		localPart,
		address,
		time.Now().Add(24*time.Hour),
	)
	if result.Error != nil {
		t.Fatalf("expected mailbox seed success for %s, got %v", localPart, result.Error)
	}

	var mailboxID uint64
	if err := db.Raw("SELECT id FROM mailboxes WHERE address = ?", address).Scan(&mailboxID).Error; err != nil {
		t.Fatalf("expected mailbox id lookup success for %s, got %v", localPart, err)
	}
	return mailboxID
}

func mustResetPersistenceTables(t *testing.T, db *gorm.DB) {
	t.Helper()

	statements := []string{
		"SET FOREIGN_KEY_CHECKS = 0",
		"DROP TABLE IF EXISTS api_key_domain_bindings",
		"DROP TABLE IF EXISTS api_key_resource_policies",
		"DROP TABLE IF EXISTS api_key_scopes",
		"DROP TABLE IF EXISTS domain_verifications",
		"DROP TABLE IF EXISTS domain_nodes",
		"DROP TABLE IF EXISTS dns_zones",
		"DROP TABLE IF EXISTS provider_accounts",
		"DROP TABLE IF EXISTS message_attachments",
		"DROP TABLE IF EXISTS messages",
		"DROP TABLE IF EXISTS mailboxes",
		"DROP TABLE IF EXISTS user_webhooks",
		"DROP TABLE IF EXISTS user_api_keys",
		"DROP TABLE IF EXISTS feedback_tickets",
		"DROP TABLE IF EXISTS notices",
		"DROP TABLE IF EXISTS user_balance_entries",
		"DROP TABLE IF EXISTS user_billing_profiles",
		"DROP TABLE IF EXISTS user_profiles",
		"DROP TABLE IF EXISTS user_roles",
		"DROP TABLE IF EXISTS roles",
		"DROP TABLE IF EXISTS refresh_tokens",
		"DROP TABLE IF EXISTS users",
		"DROP TABLE IF EXISTS domains",
		"DROP TABLE IF EXISTS mailbox_rules",
		"DROP TABLE IF EXISTS rules",
		"DROP TABLE IF EXISTS system_configs",
		"DROP TABLE IF EXISTS jobs",
		"DROP TABLE IF EXISTS audit_logs",
		"SET FOREIGN_KEY_CHECKS = 1",
	}

	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("expected reset statement success for %q, got %v", stmt, err)
		}
	}
}

func testMySQLDSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("SHIRO_TEST_MYSQL_DSN")
	if dsn == "" {
		dsn = "root:root@tcp(127.0.0.1:3306)/shiro_email_test?parseTime=true&multiStatements=true"
	}
	return dsn
}

func testRedisAddr() string {
	addr := os.Getenv("SHIRO_TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379/1"
	}
	return addr
}

func runtimeTestConfig(t *testing.T) config.Config {
	t.Helper()

	return config.Config{
		AppPort:               "8080",
		MySQLDSN:              testMySQLDSN(t),
		RedisAddr:             testRedisAddr(),
		JWTSecret:             "dev-secret",
		LegacyMailSyncAPIURL:  "http://127.0.0.1:9000",
		LegacyMailSyncEnabled: false,
	}
}

func mustEnsureTestDatabase(t *testing.T, dsn string) {
	t.Helper()

	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("expected parseable mysql dsn, got %v", err)
	}
	if cfg.DBName == "" {
		t.Fatal("expected mysql dsn to include database name")
	}

	adminCfg := *cfg
	adminCfg.DBName = ""
	adminDSN := adminCfg.FormatDSN()

	sqlDB, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatalf("expected mysql admin connection, got %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		if err := waitForTCP("127.0.0.1:3306", 3*time.Second); err != nil && strings.Contains(adminDSN, "127.0.0.1:3306") {
			t.Skipf("skipping mysql-backed persistence test: %v", err)
		}
	}

	if err := waitForMySQL(sqlDB, 3*time.Second); err != nil {
		t.Skipf("skipping mysql-backed persistence test: %v", err)
	}

	query := fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
		strings.ReplaceAll(cfg.DBName, "`", "``"),
	)
	if _, err := sqlDB.Exec(query); err != nil {
		t.Fatalf("expected create database success, got %v", err)
	}
}

func waitForMySQL(db *sql.DB, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := db.Ping(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(1 * time.Second)
	}
	return lastErr
}

func waitForRedis(ctx context.Context, client *redis.Client, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := database.PingRedis(ctx, client); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(1 * time.Second)
	}
	return lastErr
}

func waitForTCP(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(1 * time.Second)
	}
	return lastErr
}
