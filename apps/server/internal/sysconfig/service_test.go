package sysconfig

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/mail"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

func TestSysconfigServiceDefaultsAndEnv(t *testing.T) {
	cfg := &config.Config{
		MaxUploadBytes: 2 << 30, // 2GB
		AuditRetention: 180 * 24 * time.Hour,
	}

	svc := NewService(nil, cfg)
	all, err := svc.GetAll(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}

	if all[KeyInstanceName].Value != "pp-hub" || all[KeyInstanceName].Source != "default" {
		t.Errorf("instance_name expected default pp-hub, got %+v", all[KeyInstanceName])
	}
	if all[KeyMaxUploadBytes].Value != int64(2<<30) || all[KeyMaxUploadBytes].Source != "env" {
		t.Errorf("max_upload_bytes expected env fallback 2GB, got %+v", all[KeyMaxUploadBytes])
	}
	if all[KeyAuditRetentionDays].Value != 180 || all[KeyAuditRetentionDays].Source != "env" {
		t.Errorf("audit_retention_days expected env fallback 180, got %+v", all[KeyAuditRetentionDays])
	}
	if all[KeyDefaultSpaceQuotaBytes].Value != int64(0) || all[KeyDefaultSpaceQuotaBytes].Source != "default" {
		t.Errorf("default_space_quota_bytes expected default 0, got %+v", all[KeyDefaultSpaceQuotaBytes])
	}

	// Test accessors
	if svc.MaxUploadBytes("default") != int64(2<<30) {
		t.Errorf("MaxUploadBytes() accessor mismatch: %d", svc.MaxUploadBytes("default"))
	}
	if svc.AuditRetention("default") != 180*24*time.Hour {
		t.Errorf("AuditRetention() accessor mismatch: %v", svc.AuditRetention("default"))
	}
	if svc.InstanceName("default") != "pp-hub" {
		t.Errorf("InstanceName() accessor mismatch: %s", svc.InstanceName("default"))
	}
	if svc.DefaultSpaceQuotaBytes("default") != 0 {
		t.Errorf("DefaultSpaceQuotaBytes() accessor mismatch: %d", svc.DefaultSpaceQuotaBytes("default"))
	}
}

func TestSysconfigValidation(t *testing.T) {
	svc := NewService(nil, nil)

	// Unknown key
	updates := map[string]json.RawMessage{
		"unknown_key": json.RawMessage(`"val"`),
	}
	if _, err := svc.UpdateMany(context.Background(), "default", updates, pgtype.UUID{}); err == nil {
		t.Error("expected error for unknown configuration key")
	}

	// Invalid instance_name (empty)
	updates = map[string]json.RawMessage{
		KeyInstanceName: json.RawMessage(`"   "`),
	}
	if _, err := svc.UpdateMany(context.Background(), "default", updates, pgtype.UUID{}); err == nil {
		t.Error("expected error for empty instance_name")
	}

	// Invalid max_upload_bytes (below 1MB when non-zero)
	updates = map[string]json.RawMessage{
		KeyMaxUploadBytes: json.RawMessage(`500`),
	}
	if _, err := svc.UpdateMany(context.Background(), "default", updates, pgtype.UUID{}); err == nil {
		t.Error("expected error for max_upload_bytes < 1MB")
	}

	// Valid max_upload_bytes = 0 (unlimited)
	updates = map[string]json.RawMessage{
		KeyMaxUploadBytes: json.RawMessage(`0`),
	}
	if _, err := svc.UpdateMany(context.Background(), "default", updates, pgtype.UUID{}); err != nil {
		t.Errorf("expected max_upload_bytes = 0 to be valid, got: %v", err)
	}

	// Invalid audit_retention_days (zero or negative)
	updates = map[string]json.RawMessage{
		KeyAuditRetentionDays: json.RawMessage(`0`),
	}
	if _, err := svc.UpdateMany(context.Background(), "default", updates, pgtype.UUID{}); err == nil {
		t.Error("expected error for audit_retention_days < 1")
	}

	// Valid in-memory update
	updates = map[string]json.RawMessage{
		KeyInstanceName:       json.RawMessage(`"Acme Hub"`),
		KeyMaxUploadBytes:     json.RawMessage(`10485760`), // 10MB
		KeyAuditRetentionDays: json.RawMessage(`90`),
	}
	all, err := svc.UpdateMany(context.Background(), "default", updates, pgtype.UUID{})
	if err != nil {
		t.Fatalf("valid UpdateMany failed: %v", err)
	}

	if all[KeyInstanceName].Value != "Acme Hub" || all[KeyInstanceName].Source != "database" {
		t.Errorf("instance_name expected Acme Hub (database), got %+v", all[KeyInstanceName])
	}
	if all[KeyMaxUploadBytes].Value != int64(10485760) || all[KeyMaxUploadBytes].Source != "database" {
		t.Errorf("max_upload_bytes expected 10MB (database), got %+v", all[KeyMaxUploadBytes])
	}
	if all[KeyAuditRetentionDays].Value != int64(90) && all[KeyAuditRetentionDays].Value != 90 {
		t.Errorf("audit_retention_days expected 90, got %+v", all[KeyAuditRetentionDays])
	}

	if svc.InstanceName("default") != "Acme Hub" {
		t.Errorf("expected instance name Acme Hub, got %s", svc.InstanceName("default"))
	}
	if svc.MaxUploadBytes("default") != 10485760 {
		t.Errorf("expected max upload bytes 10485760, got %d", svc.MaxUploadBytes("default"))
	}
	if svc.AuditRetention("default") != 90*24*time.Hour {
		t.Errorf("expected audit retention 90 days, got %v", svc.AuditRetention("default"))
	}

	// Delete override
	if err := svc.Delete(context.Background(), "default", KeyInstanceName); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if svc.InstanceName("default") != "pp-hub" {
		t.Errorf("expected instance name to revert to pp-hub, got %s", svc.InstanceName("default"))
	}
}

func TestSysconfigDatabasePersistence(t *testing.T) {
	url := os.Getenv("PP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PP_TEST_DATABASE_URL not set; skipping database-backed test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	q := store.New(pool)
	cfg := &config.Config{
		MaxUploadBytes: 1 << 30, // 1GB
		AuditRetention: 30 * 24 * time.Hour,
	}

	svc1 := NewService(q, cfg)
	if err := svc1.Reload(ctx, "default"); err != nil {
		t.Fatalf("reload svc1: %v", err)
	}

	// Update setting in svc1
	updates := map[string]json.RawMessage{
		KeyInstanceName:   json.RawMessage(`"Postgres Hub"`),
		KeyMaxUploadBytes: json.RawMessage(`2147483648`), // 2GB
	}
	if _, err := svc1.UpdateMany(ctx, "default", updates, pgtype.UUID{}); err != nil {
		t.Fatalf("UpdateMany: %v", err)
	}

	// Simulate replica / server restart in svc2
	svc2 := NewService(q, cfg)
	if err := svc2.Reload(ctx, "default"); err != nil {
		t.Fatalf("reload svc2: %v", err)
	}

	if svc2.InstanceName("default") != "Postgres Hub" {
		t.Errorf("svc2 expected instance_name 'Postgres Hub', got %s", svc2.InstanceName("default"))
	}
	if svc2.MaxUploadBytes("default") != 2147483648 {
		t.Errorf("svc2 expected max upload 2GB, got %d", svc2.MaxUploadBytes("default"))
	}
	// Unmodified setting still falls back to env
	if svc2.AuditRetention("default") != 30*24*time.Hour {
		t.Errorf("svc2 expected audit retention 30 days, got %v", svc2.AuditRetention("default"))
	}

	// Clean up overrides
	_ = svc1.Delete(ctx, "default", KeyInstanceName)
	_ = svc1.Delete(ctx, "default", KeyMaxUploadBytes)
}

func TestSMTPPasswordIsWriteOnly(t *testing.T) {
	cfg := &config.Config{
		SecretKey:       []byte("master-secret-of-thirty-two-byte"),
		SecretKeyShared: true,
	}
	svc := NewService(nil, cfg)
	ctx := context.Background()

	updates := map[string]json.RawMessage{
		KeySMTP: json.RawMessage(`{
			"host": "smtp.example.com",
			"port": 587,
			"username": "mailer",
			"password": "supersecretpassword123",
			"from": "pp-hub <noreply@example.com>",
			"tls": "starttls"
		}`),
	}
	if _, err := svc.UpdateMany(ctx, "default", updates, pgtype.UUID{}); err != nil {
		t.Fatalf("UpdateMany failed: %v", err)
	}

	all, err := svc.GetAll(ctx, "default")
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	smtpDTO := all[KeySMTP]
	valBytes, err := json.Marshal(smtpDTO.Value)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if strings.Contains(string(valBytes), "supersecretpassword123") {
		t.Fatal("password leaked in GetAll DTO value!")
	}
	valMap, ok := smtpDTO.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any value, got %T", smtpDTO.Value)
	}
	if valMap["password_set"] != true {
		t.Fatalf("expected password_set to be true, got %v", valMap["password_set"])
	}

	effective, err := svc.SMTP()
	if err != nil {
		t.Fatalf("SMTP() failed: %v", err)
	}
	if effective.Password != "supersecretpassword123" {
		t.Fatalf("expected decrypted password %q, got %q", "supersecretpassword123", effective.Password)
	}
}

func TestSMTPOmittedPasswordIsKept(t *testing.T) {
	cfg := &config.Config{
		SecretKey:       []byte("master-secret-of-thirty-two-byte"),
		SecretKeyShared: true,
	}
	svc := NewService(nil, cfg)
	ctx := context.Background()

	// First patch sets password
	updates1 := map[string]json.RawMessage{
		KeySMTP: json.RawMessage(`{
			"host": "smtp.example.com",
			"port": 587,
			"username": "mailer",
			"password": "initialpassword",
			"from": "noreply@example.com",
			"tls": "starttls"
		}`),
	}
	if _, err := svc.UpdateMany(ctx, "default", updates1, pgtype.UUID{}); err != nil {
		t.Fatalf("UpdateMany 1 failed: %v", err)
	}

	// Second patch omits password -> keeps existing password
	updates2 := map[string]json.RawMessage{
		KeySMTP: json.RawMessage(`{
			"host": "smtp2.example.com",
			"port": 587,
			"username": "mailer",
			"from": "noreply@example.com",
			"tls": "starttls"
		}`),
	}
	if _, err := svc.UpdateMany(ctx, "default", updates2, pgtype.UUID{}); err != nil {
		t.Fatalf("UpdateMany 2 failed: %v", err)
	}
	effective, err := svc.SMTP()
	if err != nil {
		t.Fatalf("SMTP() failed: %v", err)
	}
	if effective.Host != "smtp2.example.com" {
		t.Errorf("expected host smtp2.example.com, got %s", effective.Host)
	}
	if effective.Password != "initialpassword" {
		t.Errorf("expected password initialpassword to be kept, got %s", effective.Password)
	}

	// Third patch with password: "" -> clears password
	updates3 := map[string]json.RawMessage{
		KeySMTP: json.RawMessage(`{
			"host": "smtp2.example.com",
			"port": 587,
			"username": "mailer",
			"password": "",
			"from": "noreply@example.com",
			"tls": "starttls"
		}`),
	}
	if _, err := svc.UpdateMany(ctx, "default", updates3, pgtype.UUID{}); err != nil {
		t.Fatalf("UpdateMany 3 failed: %v", err)
	}
	effective, err = svc.SMTP()
	if err != nil {
		t.Fatalf("SMTP() failed: %v", err)
	}
	if effective.Password != "" {
		t.Errorf("expected password to be cleared, got %s", effective.Password)
	}
}

func TestSMTPCrossFieldValidation(t *testing.T) {
	cfg := &config.Config{
		SecretKey:       []byte("master-secret-of-thirty-two-byte"),
		SecretKeyShared: true,
	}
	svc := NewService(nil, cfg)
	ctx := context.Background()

	updates := map[string]json.RawMessage{
		KeySMTP: json.RawMessage(`{
			"host": "smtp.example.com",
			"port": 587,
			"from": ""
		}`),
	}
	if _, err := svc.UpdateMany(ctx, "default", updates, pgtype.UUID{}); err == nil {
		t.Fatal("expected error for host without from address")
	}
}

func TestSMTPRefusesPasswordWithEphemeralKey(t *testing.T) {
	cfg := &config.Config{
		SecretKey:       []byte("master-secret-of-thirty-two-byte"),
		SecretKeyShared: false,
	}
	svc := NewService(nil, cfg)
	ctx := context.Background()

	updates := map[string]json.RawMessage{
		KeySMTP: json.RawMessage(`{
			"host": "smtp.example.com",
			"port": 587,
			"username": "mailer",
			"password": "mypassword",
			"from": "noreply@example.com"
		}`),
	}
	if _, err := svc.UpdateMany(ctx, "default", updates, pgtype.UUID{}); err == nil {
		t.Fatal("expected error when SecretKeyShared is false and password is provided")
	}
}

func TestSMTPFallsBackToEnv(t *testing.T) {
	cfg := &config.Config{
		SMTP: mail.Settings{
			Host:     "env.smtp.com",
			Port:     587,
			Username: "envuser",
			Password: "envpassword",
			From:     "env@example.com",
			TLS:      "starttls",
		},
	}
	svc := NewService(nil, cfg)
	ctx := context.Background()

	all, err := svc.GetAll(ctx, "default")
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if all[KeySMTP].Source != "env" {
		t.Fatalf("expected source env, got %s", all[KeySMTP].Source)
	}
	effective, err := svc.SMTP()
	if err != nil {
		t.Fatalf("SMTP() failed: %v", err)
	}
	if effective != cfg.SMTP {
		t.Fatalf("expected effective %v, got %v", cfg.SMTP, effective)
	}
}

// The CDN allowlist is spliced into the artifact host's CSP, so only bare
// https origins pass: a path, a wildcard, a quote or a semicolon would change
// what the policy says.
func TestArtifactCDNAllowlistValidation(t *testing.T) {
	svc := NewService(nil, nil)
	for _, bad := range []string{
		"http://cdn.example", "https://cdn.example/lib", "https://*.example", "'unsafe-inline'",
		"https://cdn.example; script-src *", "*", "https://cdn.example?x=1", "https://user@cdn.example",
		"data:", "https://cdn.example#x",
	} {
		raw, _ := json.Marshal(bad)
		if _, err := svc.UpdateMany(context.Background(), "default",
			map[string]json.RawMessage{KeyArtifactCDNAllowlist: raw}, pgtype.UUID{}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	got, err := ParseCDNAllowlist("  https://cdn.example   https://fonts.example:8443 ")
	if err != nil || len(got) != 2 || got[1] != "https://fonts.example:8443" {
		t.Fatalf("ParseCDNAllowlist = %v, %v", got, err)
	}
	if list := svc.ArtifactCDNAllowlist(); len(list) != 5 {
		t.Fatalf("default allowlist = %v", list)
	}
}
