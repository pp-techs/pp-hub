package users

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Preferences is the account-level slice of a person's settings: the things
// that are about *them* rather than about the machine they happen to be
// sitting at. It is stored in users.settings, a JSONB column that has existed
// since 0001_init.sql, so a new preference costs one field here and one in the
// client — never a migration.
//
// Every field is optional and its zero value means "never chosen", which is
// what keeps detection in charge. A locale defaulted to "en" on the server
// would silently pin every existing account to English and make the browser's
// Accept-Language irrelevant, so the empty string is load-bearing.
//
// This struct is the *read* view, not the schema of the column. Writes merge
// at the JSON level (see mergePreferences) and keep keys this binary has no
// field for, so adding a preference never risks an older code path erasing a
// newer one.
type Preferences struct {
	// Locale is a BCP-47 tag. The client narrows it to a catalog it ships and
	// falls back on its own when it cannot, so the server validates shape
	// only — an allowlist here would make adding a language a backend release.
	Locale string `json:"locale,omitempty"`
	// EmailOnShare controls transactional emails on share events.
	// Nil means never chosen, which defaults to true (opt-out).
	EmailOnShare *bool `json:"email_on_share,omitempty"`
}

func (p Preferences) WantsShareEmail() bool {
	return p.EmailOnShare == nil || *p.EmailOnShare
}

// PreferencesPatch is a partial update: a nil field is untouched. This mirrors
// handleUpdateSpace, the same shape every other PATCH in the API uses.
type PreferencesPatch struct {
	Locale       *string `json:"locale"`
	EmailOnShare *bool   `json:"email_on_share"`
}

// localePattern accepts a language subtag with an optional script or region,
// which covers everything a browser sends ("vi", "vi-VN", "zh-Hans") while
// rejecting anything that could travel onward into the OnlyOffice editor URL
// as something other than a language.
var localePattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z]{2,4})?$`)

// ValidLocale reports whether a tag is safe to store and to pass on.
func ValidLocale(tag string) bool {
	return tag == "" || (len(tag) <= 16 && localePattern.MatchString(tag))
}

// DecodePreferences reads the JSONB column. A malformed or empty document is
// not an error: it predates this type, or a hand-edited row put junk there,
// and in both cases "no preferences" is the honest answer.
func DecodePreferences(raw []byte) Preferences {
	var p Preferences
	if len(raw) == 0 {
		return p
	}
	_ = json.Unmarshal(raw, &p)
	return p
}

// Preferences returns the caller's stored preferences.
func (s *Service) Preferences(ctx context.Context, id pgtype.UUID) (Preferences, error) {
	u, err := s.q.GetUser(ctx, id)
	if err != nil {
		return Preferences{}, ErrNotFound
	}
	return DecodePreferences(u.Settings), nil
}

// mergePreferences applies a patch to the raw stored document.
//
// It merges at the JSON level rather than round-tripping through the
// `Preferences` struct, so a key this binary has never heard of survives the
// write. Marshalling the struct back would silently drop it — which turns
// "add a preference" into "and make sure nothing older ever writes again",
// and this column is explicitly the place preferences are meant to accumulate.
// A document that is absent or not an object starts from empty; junk in the
// column is not a reason to refuse a language change.
func mergePreferences(raw []byte, patch PreferencesPatch) ([]byte, error) {
	doc := map[string]json.RawMessage{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &doc)
	}
	set := func(key string, value any) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		doc[key] = encoded
		return nil
	}
	if patch.Locale != nil {
		if err := set("locale", strings.TrimSpace(*patch.Locale)); err != nil {
			return nil, err
		}
	}
	if patch.EmailOnShare != nil {
		if err := set("email_on_share", *patch.EmailOnShare); err != nil {
			return nil, err
		}
	}
	return json.Marshal(doc)
}

// UpdatePreferences applies a partial patch and returns the whole document.
//
// Read-modify-write against users.settings, so a client that knows about only
// one preference cannot erase the ones it has never heard of — which is the
// property that lets this grow without versioning the column.
func (s *Service) UpdatePreferences(ctx context.Context, id pgtype.UUID, patch PreferencesPatch) (Preferences, error) {
	u, err := s.q.GetUser(ctx, id)
	if err != nil {
		return Preferences{}, ErrNotFound
	}
	blob, err := mergePreferences(u.Settings, patch)
	if err != nil {
		return Preferences{}, err
	}
	updated, err := s.q.UpdateUserSettings(ctx, store.UpdateUserSettingsParams{
		ID:       id,
		Settings: blob,
	})
	if err != nil {
		return Preferences{}, err
	}
	return DecodePreferences(updated.Settings), nil
}
