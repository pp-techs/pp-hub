package sysconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/mail"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

const (
	KeyInstanceName                  = "instance_name"
	KeyMaxUploadBytes                = "max_upload_bytes"
	KeyAuditRetentionDays            = "audit_retention_days"
	KeyReadAuditRetentionDays        = "read_audit_retention_days"
	KeyAlertFailedLoginThreshold     = "alert_failed_login_threshold"
	KeyAlertFailedLoginWindowMinutes = "alert_failed_login_window_minutes"
	KeyDefaultSpaceQuotaBytes        = "default_space_quota_bytes"
	KeyVersionRetentionCount         = "version_retention_count"
	KeySMTP                          = "smtp"
	// KeyMCPEnabled lets people connect AI agents to their account through
	// the MCP endpoint (M41). Off by default: an instance turns it on once
	// pp-logto-setup.sh has registered the resource and enabled CIMD.
	KeyMCPEnabled = "mcp_enabled"
	// KeyMCPPublicLinks lets an agent mint public links — the one grant that
	// reaches people outside the space, and so the one a prompt-injected
	// document would aim for. Off by default even when MCP is on.
	KeyMCPPublicLinks = "mcp_public_links"
	// KeyMCPTokens lets people mint personal access tokens for agents that
	// cannot sign in through a browser (a .mcp.json, CI). On by default:
	// agent access as a whole is already the instance's opt-in.
	KeyMCPTokens = "mcp_tokens"
	// KeyMCPExternalShares lets an agent give access to someone outside the
	// node's space — an external grant or an invitation. Off by default for
	// the reason public links are: it is what a prompt-injected document
	// would aim for.
	KeyMCPExternalShares = "mcp_external_shares"
	// KeyPersonalSpaces decides who gets a personal space at sign-in:
	// PersonalSpacesOff, PersonalSpacesMembers or PersonalSpacesEveryone.
	KeyPersonalSpaces = "personal_spaces"
	// KeyDefaultPersonalQuotaBytes is the quota a new personal space gets.
	KeyDefaultPersonalQuotaBytes = "default_personal_quota_bytes"
	// KeyExternalSharingEnabled is the instance-wide gate on external grants
	// and invitations; each space has its own switch beneath it.
	KeyExternalSharingEnabled = "external_sharing_enabled"

	DefaultTenantID = "default"
)

// Values of KeyPersonalSpaces.
const (
	// PersonalSpacesOff provisions nobody.
	PersonalSpacesOff = "off"
	// PersonalSpacesMembers provisions people who belong to at least one
	// organization — a company's self-host, where Logto's open email sign-up
	// exists only so invitees can create their account.
	PersonalSpacesMembers = "members"
	// PersonalSpacesEveryone provisions every account — a public instance.
	PersonalSpacesEveryone = "everyone"
)

// SettingDef describes a known system configuration knob.
type SettingDef struct {
	Key         string                       `json:"key"`
	Type        string                       `json:"type"` // "string" | "number" | "boolean" | "smtp"
	Description string                       `json:"description"`
	Default     any                          `json:"default"`
	EnvFallback func(cfg *config.Config) any `json:"-"`
	Validate    func(val any) error          `json:"-"`
}

// SettingDTO represents a configuration entry returned over the API.
type SettingDTO struct {
	Key         string `json:"key"`
	Value       any    `json:"value"`
	Source      string `json:"source"` // "database" | "env" | "default"
	Description string `json:"description"`
	Default     any    `json:"default"`
	Type        string `json:"type"`
}

type storedSMTP struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PasswordEnc string `json:"password_enc,omitempty"`
	From        string `json:"from"`
	TLS         string `json:"tls"`
}

type patchSMTP struct {
	Host     string  `json:"host"`
	Port     int     `json:"port"`
	Username string  `json:"username"`
	Password *string `json:"password"`
	From     string  `json:"from"`
	TLS      string  `json:"tls"`
}

func smtpPublic(host string, port int, username, from, tls string, passwordSet bool) map[string]any {
	return map[string]any{
		"host":         host,
		"port":         port,
		"username":     username,
		"from":         from,
		"tls":          tls,
		"password_set": passwordSet,
	}
}

// Service manages system-wide dynamic configuration backed by Postgres
// with in-memory caching and environment variable fallbacks.
type Service struct {
	store *store.Queries
	cfg   *config.Config
	defs  map[string]SettingDef

	mu    sync.RWMutex
	cache map[string][]byte
}

func defaultDefs() map[string]SettingDef {
	return map[string]SettingDef{
		KeyInstanceName: {
			Key:         KeyInstanceName,
			Type:        "string",
			Description: "Display name for this instance",
			Default:     "pp-hub",
			Validate: func(val any) error {
				s, ok := val.(string)
				if !ok {
					return errors.New("instance_name must be a string")
				}
				s = strings.TrimSpace(s)
				if len(s) == 0 {
					return errors.New("instance_name cannot be empty")
				}
				if len(s) > 64 {
					return errors.New("instance_name cannot exceed 64 characters")
				}
				return nil
			},
		},
		KeyMaxUploadBytes: {
			Key:         KeyMaxUploadBytes,
			Type:        "number",
			Description: "Maximum allowed file size per upload in bytes (0 = unlimited)",
			Default:     int64(5 << 30), // 5GB
			EnvFallback: func(c *config.Config) any {
				if c != nil {
					return c.MaxUploadBytes
				}
				return nil
			},
			Validate: func(val any) error {
				n, ok := val.(int64)
				if !ok {
					return errors.New("max_upload_bytes must be an integer")
				}
				if n < 0 {
					return errors.New("max_upload_bytes cannot be negative")
				}
				if n > 0 && n < 1048576 {
					return errors.New("max_upload_bytes must be at least 1MB (or 0 for unlimited)")
				}
				if n > 1<<40 { // 1TB
					return errors.New("max_upload_bytes cannot exceed 1TB")
				}
				return nil
			},
		},
		KeyAuditRetentionDays: {
			Key:         KeyAuditRetentionDays,
			Type:        "number",
			Description: "Days to retain security events before automatic purge",
			Default:     365,
			EnvFallback: func(c *config.Config) any {
				if c != nil && c.AuditRetention > 0 {
					return int(c.AuditRetention / (24 * time.Hour))
				}
				return nil
			},
			Validate: func(val any) error {
				n, ok := val.(int)
				if !ok {
					return errors.New("audit_retention_days must be an integer")
				}
				if n < 1 {
					return errors.New("audit_retention_days must be at least 1 day")
				}
				if n > 3650 {
					return errors.New("audit_retention_days cannot exceed 3650 days (10 years)")
				}
				return nil
			},
		},
		KeyReadAuditRetentionDays: {
			Key:         KeyReadAuditRetentionDays,
			Type:        "number",
			Description: "Days to retain file access events (downloads, views, editor opens) before automatic purge",
			Default:     90,
			Validate: func(val any) error {
				n, ok := val.(int)
				if !ok {
					return errors.New("read_audit_retention_days must be an integer")
				}
				if n < 1 {
					return errors.New("read_audit_retention_days must be at least 1 day")
				}
				if n > 3650 {
					return errors.New("read_audit_retention_days cannot exceed 3650 days (10 years)")
				}
				return nil
			},
		},
		KeyAlertFailedLoginThreshold: {
			Key:         KeyAlertFailedLoginThreshold,
			Type:        "number",
			Description: "Failed sign-ins from one IP address within the alert window that raise a security alert (0 disables alerts)",
			Default:     10,
			Validate: func(val any) error {
				n, ok := val.(int)
				if !ok {
					return errors.New("alert_failed_login_threshold must be an integer")
				}
				if n < 0 || n > 10000 {
					return errors.New("alert_failed_login_threshold must be between 0 and 10000")
				}
				return nil
			},
		},
		KeyAlertFailedLoginWindowMinutes: {
			Key:         KeyAlertFailedLoginWindowMinutes,
			Type:        "number",
			Description: "Minutes of failed sign-ins counted toward the alert threshold",
			Default:     15,
			Validate: func(val any) error {
				n, ok := val.(int)
				if !ok {
					return errors.New("alert_failed_login_window_minutes must be an integer")
				}
				if n < 1 || n > 1440 {
					return errors.New("alert_failed_login_window_minutes must be between 1 and 1440")
				}
				return nil
			},
		},
		KeyDefaultSpaceQuotaBytes: {
			Key:         KeyDefaultSpaceQuotaBytes,
			Type:        "number",
			Description: "Default storage quota in bytes for newly created spaces (0 = unlimited)",
			Default:     int64(0),
			Validate: func(val any) error {
				n, ok := val.(int64)
				if !ok {
					return errors.New("default_space_quota_bytes must be an integer")
				}
				if n < 0 {
					return errors.New("default_space_quota_bytes cannot be negative")
				}
				return nil
			},
		},
		KeyVersionRetentionCount: {
			Key:         KeyVersionRetentionCount,
			Type:        "number",
			Description: "Versions kept per file, current included; older ones are deleted when a new version is saved (0 keeps every version)",
			// Google Drive's figure. Unbounded history is what let the disk
			// fill while quota counted only current versions.
			Default: 100,
			Validate: func(val any) error {
				n, ok := val.(int)
				if !ok {
					return errors.New("version_retention_count must be an integer")
				}
				if n < 0 || n > 10000 {
					return errors.New("version_retention_count must be between 0 and 10000")
				}
				return nil
			},
		},
		KeySMTP: {
			Key:         KeySMTP,
			Type:        "smtp",
			Description: "Outgoing mail relay used for share notifications",
			Default:     smtpPublic("", 587, "", "", "starttls", false),
			EnvFallback: func(c *config.Config) any {
				if c != nil && c.SMTP.Host != "" {
					return smtpPublic(c.SMTP.Host, c.SMTP.Port, c.SMTP.Username, c.SMTP.From, c.SMTP.TLS, c.SMTP.Password != "")
				}
				return nil
			},
			Validate: nil,
		},
		KeyMCPEnabled: {
			Key:         KeyMCPEnabled,
			Type:        "boolean",
			Description: "Let people connect AI agents (MCP) to their account",
			Default:     false,
		},
		KeyMCPPublicLinks: {
			Key:         KeyMCPPublicLinks,
			Type:        "boolean",
			Description: "Let connected agents create public links",
			Default:     false,
		},
		KeyMCPTokens: {
			Key:         KeyMCPTokens,
			Type:        "boolean",
			Description: "Let people create personal access tokens for agents",
			Default:     true,
		},
		KeyMCPExternalShares: {
			Key:         KeyMCPExternalShares,
			Type:        "boolean",
			Description: "Let connected agents give access to people outside a space",
			Default:     false,
		},
		KeyPersonalSpaces: {
			Key:         KeyPersonalSpaces,
			Type:        "string",
			Description: "Who gets a personal space at sign-in: off, members (people in an organization) or everyone",
			Default:     PersonalSpacesOff,
			Validate: func(val any) error {
				switch val {
				case PersonalSpacesOff, PersonalSpacesMembers, PersonalSpacesEveryone:
					return nil
				}
				return errors.New("personal_spaces must be off, members or everyone")
			},
		},
		KeyDefaultPersonalQuotaBytes: {
			Key:         KeyDefaultPersonalQuotaBytes,
			Type:        "number",
			Description: "Storage quota in bytes for a new personal space (0 = unlimited; required when personal spaces are open to everyone)",
			Default:     int64(0),
			Validate: func(val any) error {
				n, ok := val.(int64)
				if !ok {
					return errors.New("default_personal_quota_bytes must be an integer")
				}
				if n < 0 {
					return errors.New("default_personal_quota_bytes cannot be negative")
				}
				return nil
			},
		},
		KeyExternalSharingEnabled: {
			Key:         KeyExternalSharingEnabled,
			Type:        "boolean",
			Description: "Let people give access to someone outside a space by email",
			Default:     false,
		},
	}
}

// NewService creates a new sysconfig Service.
func NewService(q *store.Queries, cfg *config.Config) *Service {
	return &Service{
		store: q,
		cfg:   cfg,
		defs:  defaultDefs(),
		cache: make(map[string][]byte),
	}
}

// Reload re-reads all settings from the database for the given tenant.
func (s *Service) Reload(ctx context.Context, tenantID string) error {
	if s.store == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	rows, err := s.store.ListSystemConfig(ctx, tenantID)
	if err != nil {
		return err
	}

	newCache := make(map[string][]byte, len(rows))
	for _, row := range rows {
		newCache[row.Key] = row.Value
	}

	s.mu.Lock()
	s.cache = newCache
	s.mu.Unlock()
	return nil
}

func decodeSettingValue(def SettingDef, raw []byte) any {
	switch def.Key {
	case KeyInstanceName:
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			return str
		}
	case KeyMaxUploadBytes, KeyDefaultSpaceQuotaBytes, KeyDefaultPersonalQuotaBytes:
		var num int64
		if err := json.Unmarshal(raw, &num); err == nil {
			return num
		}
	case KeyAuditRetentionDays, KeyReadAuditRetentionDays,
		KeyAlertFailedLoginThreshold, KeyAlertFailedLoginWindowMinutes,
		KeyVersionRetentionCount:
		var num int
		if err := json.Unmarshal(raw, &num); err == nil {
			return num
		}
	case KeySMTP:
		var st storedSMTP
		if err := json.Unmarshal(raw, &st); err == nil {
			return smtpPublic(st.Host, st.Port, st.Username, st.From, st.TLS, st.PasswordEnc != "")
		}
	default:
		var val any
		if err := json.Unmarshal(raw, &val); err == nil {
			return val
		}
	}
	return def.Default
}

// coerceAndValidate parses raw JSON bytes into the strongly-typed setting value and runs validation.
func (s *Service) coerceAndValidate(def SettingDef, raw json.RawMessage) (any, []byte, error) {
	var val any
	switch def.Key {
	case KeyInstanceName:
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return nil, nil, fmt.Errorf("%s must be a string: %w", def.Key, err)
		}
		str = strings.TrimSpace(str)
		val = str
	case KeyMaxUploadBytes, KeyDefaultSpaceQuotaBytes, KeyDefaultPersonalQuotaBytes:
		var num int64
		if err := json.Unmarshal(raw, &num); err != nil {
			return nil, nil, fmt.Errorf("%s must be an integer: %w", def.Key, err)
		}
		val = num
	case KeyAuditRetentionDays, KeyReadAuditRetentionDays,
		KeyAlertFailedLoginThreshold, KeyAlertFailedLoginWindowMinutes,
		KeyVersionRetentionCount:
		var num int
		if err := json.Unmarshal(raw, &num); err != nil {
			return nil, nil, fmt.Errorf("%s must be an integer: %w", def.Key, err)
		}
		val = num
	case KeySMTP:
		return s.coerceSMTP(raw)
	case KeyMCPEnabled, KeyMCPPublicLinks, KeyMCPTokens, KeyMCPExternalShares, KeyExternalSharingEnabled:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, nil, fmt.Errorf("%s must be true or false: %w", def.Key, err)
		}
		val = b
	default:
		if def.Type == "string" {
			var str string
			if err := json.Unmarshal(raw, &str); err != nil {
				return nil, nil, fmt.Errorf("%s must be a string: %w", def.Key, err)
			}
			val = strings.TrimSpace(str)
		} else {
			var num int64
			if err := json.Unmarshal(raw, &num); err != nil {
				return nil, nil, err
			}
			val = num
		}
	}

	if def.Validate != nil {
		if err := def.Validate(val); err != nil {
			return nil, nil, err
		}
	}

	canonicalJSON, err := json.Marshal(val)
	if err != nil {
		return nil, nil, err
	}

	return val, canonicalJSON, nil
}

// GetAll returns all registered settings resolved with current values and source.
func (s *Service) GetAll(ctx context.Context, tenantID string) (map[string]SettingDTO, error) {
	if tenantID == "" {
		tenantID = DefaultTenantID
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]SettingDTO, len(s.defs))
	for key, def := range s.defs {
		out[key] = s.resolveLocked(key, def)
	}

	return out, nil
}

// UpdateMany applies a batch of setting updates atomically.
func (s *Service) UpdateMany(ctx context.Context, tenantID string, updates map[string]json.RawMessage, updatedBy pgtype.UUID) (map[string]SettingDTO, error) {
	if tenantID == "" {
		tenantID = DefaultTenantID
	}

	// First pass: validate all updates before persisting
	validated := make(map[string]struct {
		def   SettingDef
		val   any
		bytes []byte
	}, len(updates))

	for key, raw := range updates {
		def, ok := s.defs[key]
		if !ok {
			return nil, fmt.Errorf("unknown configuration key: %q", key)
		}
		val, canonicalJSON, err := s.coerceAndValidate(def, raw)
		if err != nil {
			return nil, err
		}
		validated[key] = struct {
			def   SettingDef
			val   any
			bytes []byte
		}{def: def, val: val, bytes: canonicalJSON}
	}
	if err := s.checkCombination(validated[KeyPersonalSpaces].val, validated[KeyDefaultPersonalQuotaBytes].val,
		updates[KeyPersonalSpaces] != nil, updates[KeyDefaultPersonalQuotaBytes] != nil); err != nil {
		return nil, err
	}

	// Second pass: save to database and cache
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, item := range validated {
		if s.store != nil {
			_, err := s.store.UpsertSystemConfig(ctx, store.UpsertSystemConfigParams{
				Key:       key,
				TenantID:  tenantID,
				Value:     item.bytes,
				UpdatedBy: updatedBy,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to save setting %q: %w", key, err)
			}
		}
		s.cache[key] = item.bytes
	}

	// Return updated settings
	out := make(map[string]SettingDTO, len(s.defs))
	for key, def := range s.defs {
		out[key] = s.resolveLocked(key, def)
	}

	return out, nil
}

// Delete removes a setting override from the database, reverting to env or default.
func (s *Service) Delete(ctx context.Context, tenantID string, key string) error {
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if _, ok := s.defs[key]; !ok {
		return fmt.Errorf("unknown configuration key: %q", key)
	}
	if key == KeyDefaultPersonalQuotaBytes {
		if err := s.checkCombination(nil, int64(0), false, true); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.store != nil {
		err := s.store.DeleteSystemConfig(ctx, store.DeleteSystemConfigParams{
			Key:      key,
			TenantID: tenantID,
		})
		if err != nil {
			return err
		}
	}
	delete(s.cache, key)
	return nil
}

// MaxUploadBytes returns the effective max upload size in bytes (0 = unlimited).
func (s *Service) MaxUploadBytes(tenantID string) int64 {
	s.mu.RLock()
	raw, ok := s.cache[KeyMaxUploadBytes]
	s.mu.RUnlock()

	if ok && len(raw) > 0 {
		var n int64
		if err := json.Unmarshal(raw, &n); err == nil {
			return n
		}
	}
	if s.cfg != nil {
		return s.cfg.MaxUploadBytes
	}
	return int64(5 << 30)
}

// AuditRetention returns the effective security event retention duration.
func (s *Service) AuditRetention(tenantID string) time.Duration {
	s.mu.RLock()
	raw, ok := s.cache[KeyAuditRetentionDays]
	s.mu.RUnlock()

	if ok && len(raw) > 0 {
		var days int
		if err := json.Unmarshal(raw, &days); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour
		}
	}
	if s.cfg != nil && s.cfg.AuditRetention > 0 {
		return s.cfg.AuditRetention
	}
	return 365 * 24 * time.Hour
}

// cachedInt returns the cached integer value of key, if one decodes.
func (s *Service) cachedInt(key string) (int, bool) {
	s.mu.RLock()
	raw, ok := s.cache[key]
	s.mu.RUnlock()
	if !ok || len(raw) == 0 {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	return n, true
}

// ReadAuditRetention returns how long file access events are kept.
func (s *Service) ReadAuditRetention(tenantID string) time.Duration {
	if days, ok := s.cachedInt(KeyReadAuditRetentionDays); ok && days > 0 {
		return time.Duration(days) * 24 * time.Hour
	}
	return 90 * 24 * time.Hour
}

// FailedLoginAlertThreshold returns how many failed sign-ins from one address
// within the alert window raise an alert. Zero means alerts are off.
func (s *Service) FailedLoginAlertThreshold(tenantID string) int {
	if n, ok := s.cachedInt(KeyAlertFailedLoginThreshold); ok {
		return n
	}
	return 10
}

// FailedLoginAlertWindow returns the span of failed sign-ins counted toward
// the alert threshold.
func (s *Service) FailedLoginAlertWindow(tenantID string) time.Duration {
	if m, ok := s.cachedInt(KeyAlertFailedLoginWindowMinutes); ok && m > 0 {
		return time.Duration(m) * time.Minute
	}
	return 15 * time.Minute
}

// VersionRetentionCount returns how many versions of each file are kept,
// current included. Zero keeps every version.
func (s *Service) VersionRetentionCount(tenantID string) int {
	if n, ok := s.cachedInt(KeyVersionRetentionCount); ok && n >= 0 {
		return n
	}
	return 100
}

// InstanceName returns the configured display name of the instance.
func (s *Service) InstanceName(tenantID string) string {
	s.mu.RLock()
	raw, ok := s.cache[KeyInstanceName]
	s.mu.RUnlock()

	if ok && len(raw) > 0 {
		var str string
		if err := json.Unmarshal(raw, &str); err == nil && str != "" {
			return str
		}
	}
	return "pp-hub"
}

// DefaultSpaceQuotaBytes returns the default quota in bytes for newly created spaces.
func (s *Service) DefaultSpaceQuotaBytes(tenantID string) int64 {
	s.mu.RLock()
	raw, ok := s.cache[KeyDefaultSpaceQuotaBytes]
	s.mu.RUnlock()

	if ok && len(raw) > 0 {
		var n int64
		if err := json.Unmarshal(raw, &n); err == nil {
			return n
		}
	}
	return 0
}

// MCPEnabled reports whether agents may connect through the MCP endpoint.
func (s *Service) MCPEnabled() bool { return s.cachedBool(KeyMCPEnabled, false) }

// MCPPublicLinks reports whether connected agents may create public links.
func (s *Service) MCPPublicLinks() bool { return s.cachedBool(KeyMCPPublicLinks, false) }

// MCPTokens reports whether personal access tokens may be minted and used.
func (s *Service) MCPTokens() bool { return s.cachedBool(KeyMCPTokens, true) }

// MCPExternalShares reports whether connected agents may give access to
// people outside a space.
func (s *Service) MCPExternalShares() bool { return s.cachedBool(KeyMCPExternalShares, false) }

// ExternalSharingEnabled reports the instance-wide gate on external grants.
func (s *Service) ExternalSharingEnabled() bool {
	return s.cachedBool(KeyExternalSharingEnabled, false)
}

// PersonalSpaces returns who gets a personal space at sign-in.
func (s *Service) PersonalSpaces() string {
	s.mu.RLock()
	raw, ok := s.cache[KeyPersonalSpaces]
	s.mu.RUnlock()
	var v string
	if !ok || json.Unmarshal(raw, &v) != nil {
		return PersonalSpacesOff
	}
	switch v {
	case PersonalSpacesMembers, PersonalSpacesEveryone:
		return v
	}
	return PersonalSpacesOff
}

// DefaultPersonalQuotaBytes returns the quota a new personal space gets.
func (s *Service) DefaultPersonalQuotaBytes() int64 {
	s.mu.RLock()
	raw, ok := s.cache[KeyDefaultPersonalQuotaBytes]
	s.mu.RUnlock()
	var n int64
	if !ok || json.Unmarshal(raw, &n) != nil {
		return 0
	}
	return n
}

// checkCombination refuses opening personal spaces to everyone without a
// quota: an unbounded drive handed to anybody who can sign up is a free file
// host. Values not in this batch are read from the current settings.
func (s *Service) checkCombination(mode, quota any, modeSet, quotaSet bool) error {
	m, _ := mode.(string)
	if !modeSet {
		m = s.PersonalSpaces()
	}
	q, _ := quota.(int64)
	if !quotaSet {
		q = s.DefaultPersonalQuotaBytes()
	}
	if m == PersonalSpacesEveryone && q <= 0 {
		return errors.New("set default_personal_quota_bytes above 0 before opening personal spaces to everyone")
	}
	return nil
}

// cachedBool is the cached boolean value of key, or def when none is stored
// or it does not decode.
func (s *Service) cachedBool(key string, def bool) bool {
	s.mu.RLock()
	raw, ok := s.cache[key]
	s.mu.RUnlock()
	var b bool
	if !ok || json.Unmarshal(raw, &b) != nil {
		return def
	}
	return b
}

func (s *Service) resolveLocked(key string, def SettingDef) SettingDTO {
	dto := SettingDTO{
		Key:         key,
		Type:        def.Type,
		Description: def.Description,
		Default:     def.Default,
	}

	if raw, ok := s.cache[key]; ok && len(raw) > 0 {
		dto.Value = decodeSettingValue(def, raw)
		dto.Source = "database"
		return dto
	}

	if def.EnvFallback != nil {
		if envVal := def.EnvFallback(s.cfg); envVal != nil {
			dto.Value = envVal
			dto.Source = "env"
			return dto
		}
	}

	dto.Value = def.Default
	dto.Source = "default"
	return dto
}

func (s *Service) coerceSMTP(raw json.RawMessage) (any, []byte, error) {
	var p patchSMTP
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, nil, fmt.Errorf("smtp must be an object: %w", err)
	}
	p.Host = strings.TrimSpace(p.Host)
	p.Username = strings.TrimSpace(p.Username)
	p.From = strings.TrimSpace(p.From)
	if p.TLS == "" {
		p.TLS = "starttls"
	}
	if p.Port == 0 {
		p.Port = 587
	}

	s.mu.RLock()
	existingRaw, hasExisting := s.cache[KeySMTP]
	s.mu.RUnlock()

	var existing storedSMTP
	hasOverride := false
	if hasExisting && len(existingRaw) > 0 {
		if err := json.Unmarshal(existingRaw, &existing); err == nil {
			hasOverride = true
		}
	}

	var passwordEnc string
	if p.Password == nil {
		if hasOverride {
			passwordEnc = existing.PasswordEnc
		} else if s.cfg != nil && s.cfg.SMTP.Password != "" && p.Username != "" {
			return nil, nil, errors.New("enter the SMTP password: saving here replaces the environment configuration, and its password is not copied")
		} else {
			passwordEnc = ""
		}
	} else if *p.Password == "" {
		passwordEnc = ""
	} else {
		if s.cfg == nil || !s.cfg.SecretKeyShared {
			return nil, nil, errors.New("PP_SESSION_KEY must be set before an SMTP password can be stored: the key that encrypts it would not survive a restart")
		}
		enc, err := sealSecret(s.smtpKey(), *p.Password)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encrypt smtp password: %w", err)
		}
		passwordEnc = enc
	}

	dummyPassword := ""
	if passwordEnc != "" {
		dummyPassword = "x"
	}
	toValidate := mail.Settings{
		Host:     p.Host,
		Port:     p.Port,
		Username: p.Username,
		Password: dummyPassword,
		From:     p.From,
		TLS:      p.TLS,
	}
	if err := toValidate.Validate(); err != nil {
		return nil, nil, err
	}

	st := storedSMTP{
		Host:        p.Host,
		Port:        p.Port,
		Username:    p.Username,
		PasswordEnc: passwordEnc,
		From:        p.From,
		TLS:         p.TLS,
	}
	canonicalJSON, err := json.Marshal(st)
	if err != nil {
		return nil, nil, err
	}
	publicMap := smtpPublic(st.Host, st.Port, st.Username, st.From, st.TLS, st.PasswordEnc != "")
	return publicMap, canonicalJSON, nil
}

// SMTP returns the effective relay: the admin override, else PP_SMTP_*, else disabled.
func (s *Service) SMTP() (mail.Settings, error) {
	s.mu.RLock()
	raw, ok := s.cache[KeySMTP]
	s.mu.RUnlock()

	if ok && len(raw) > 0 {
		var st storedSMTP
		if err := json.Unmarshal(raw, &st); err != nil {
			return mail.Settings{}, fmt.Errorf("invalid stored smtp setting: %w", err)
		}
		password := ""
		if st.PasswordEnc != "" {
			dec, err := openSecret(s.smtpKey(), st.PasswordEnc)
			if err != nil {
				return mail.Settings{}, errors.New("the stored SMTP password cannot be decrypted (was PP_SESSION_KEY changed?); enter it again in the admin console")
			}
			password = dec
		}
		return mail.Settings{
			Host:     st.Host,
			Port:     st.Port,
			Username: st.Username,
			Password: password,
			From:     st.From,
			TLS:      st.TLS,
		}, nil
	}

	if s.cfg != nil {
		return s.cfg.SMTP, nil
	}
	return mail.Settings{}, nil
}
