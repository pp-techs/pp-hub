package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/sysconfig"
)

func newTestServerWithSysconfig(cfg *config.Config) *Server {
	sys := sysconfig.NewService(nil, cfg)
	return &Server{
		sysconfig:      sys,
		audit:          audit.New(nil),
		maxUploadBytes: 5 << 30,
	}
}

func withTestUser(req *http.Request, isAdmin bool) *http.Request {
	user := store.User{
		ID:        pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4}, Valid: true},
		Email:     "admin@example.com",
		Name:      "Admin",
		IsAdmin:   isAdmin,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	sess := &auth.Session{
		ID:   user.ID,
		User: user,
	}
	return req.WithContext(context.WithValue(req.Context(), sessionCtxKey, sess))
}

func TestAdminSettingsAccessControl(t *testing.T) {
	s := newTestServerWithSysconfig(&config.Config{MaxUploadBytes: 10 << 30})

	// 1. GET /api/v1/admin/settings without admin -> 403
	req := httptest.NewRequest("GET", "/api/v1/admin/settings", nil)
	req = withTestUser(req, false)
	rec := httptest.NewRecorder()
	s.handleGetAdminSettings(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", rec.Code)
	}

	// 2. GET /api/v1/admin/settings with admin -> 200
	req = httptest.NewRequest("GET", "/api/v1/admin/settings", nil)
	req = withTestUser(req, true)
	rec = httptest.NewRecorder()
	s.handleGetAdminSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d: %s", rec.Code, rec.Body.String())
	}

	var settings map[string]sysconfig.SettingDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
		t.Fatalf("failed to decode settings: %v", err)
	}
	if settings[sysconfig.KeyMaxUploadBytes].Source != "env" {
		t.Errorf("expected max_upload_bytes source to be 'env', got %s", settings[sysconfig.KeyMaxUploadBytes].Source)
	}
}

func TestAdminSettingsUpdateAndDelete(t *testing.T) {
	s := newTestServerWithSysconfig(&config.Config{MaxUploadBytes: 5 << 30})

	// 1. Invalid payload -> 400
	req := httptest.NewRequest("PATCH", "/api/v1/admin/settings", bytes.NewBufferString(`{"max_upload_bytes": 100}`))
	req = withTestUser(req, true)
	rec := httptest.NewRecorder()
	s.handleUpdateAdminSettings(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for max_upload_bytes < 1MB, got %d", rec.Code)
	}

	// 2. Valid update -> 200
	body := `{"max_upload_bytes": 20971520, "instance_name": "Custom Hub"}`
	req = httptest.NewRequest("PATCH", "/api/v1/admin/settings", bytes.NewBufferString(body))
	req = withTestUser(req, true)
	rec = httptest.NewRecorder()
	s.handleUpdateAdminSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Dynamic uploadCap check
	if s.uploadCap() != 20971520 {
		t.Errorf("expected uploadCap to reflect 20971520, got %d", s.uploadCap())
	}

	var updated map[string]sysconfig.SettingDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to decode updated settings: %v", err)
	}
	if updated[sysconfig.KeyInstanceName].Value != "Custom Hub" {
		t.Errorf("expected instance_name 'Custom Hub', got %v", updated[sysconfig.KeyInstanceName].Value)
	}

	// 3. Delete override -> reverts to default
	req = httptest.NewRequest("DELETE", "/api/v1/admin/settings/instance_name", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("key", "instance_name")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withTestUser(req, true)
	rec = httptest.NewRecorder()
	s.handleDeleteAdminSetting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete, got %d: %s", rec.Code, rec.Body.String())
	}

	if s.sysconfig.InstanceName("default") != "pp-hub" {
		t.Errorf("expected reverted instance_name 'pp-hub', got %s", s.sysconfig.InstanceName("default"))
	}
}
