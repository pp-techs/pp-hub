package sysconfig

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
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
