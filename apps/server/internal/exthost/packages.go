package exthost

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/apppkg"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Installing, upgrading and removing apps (M51): an administrator uploads a
// package and it is installed into the running registry, with no rebuild.
// The registry keeps what each app needs to answer requests; its files stay
// in ext_package_files and are read per request by the asset handler.

// AssetPath is where apps' files are served, under the asset base:
// <base>/x/<id>/<digest>/<path>.
const AssetPath = "/x"

var (
	// ErrInstalled is an upload of an id already installed, not marked as
	// an upgrade.
	ErrInstalled = errors.New("an app with this id is already installed; upload it as an update")
	// ErrNoSecretKey is a backend secret asked for where the server has no
	// key to seal it with.
	ErrNoSecretKey = errors.New("this server cannot store app secrets")
)

// InvalidError is a request the administrator can fix: a bad backend URL.
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

// pkgState is what the registry holds of an installed app.
type pkgState struct {
	m           apppkg.Manifest
	digest      string
	size        int64
	backendURL  string // the administrator's override; "" follows the manifest
	secret      string // "" until generated
	installedAt time.Time
	installedBy pgtype.UUID
	updatedAt   time.Time
	updatedBy   pgtype.UUID
}

// backend is the address requests are proxied to; "" when the app has none.
func (p *pkgState) backend() string {
	if p.backendURL != "" {
		return p.backendURL
	}
	if p.m.Backend != nil {
		return p.m.Backend.URL
	}
	return ""
}

// PackageInfo is what the SPA needs to show an app: there is no code for it
// in the bundle.
type PackageInfo struct {
	Digest        string
	EntryURL      string
	IconURL       string
	Kinds         []KindInfo
	Notifications map[string]string
	Translations  map[string]map[string]string
	HasBackend    bool
	Connect       []string
}

// KindInfo is one node kind of an app, as the New menu and the icons show it.
type KindInfo struct {
	Kind, Label, NewLabel, DefaultName, NewHint, IconURL string
}

// AssetBase is where apps' files are served from, without a trailing slash.
func (r *Registry) AssetBase() string { return r.cfg.AssetBase }

// AssetURL is the address of one of an app's files.
func (r *Registry) AssetURL(id, digest, path string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return r.cfg.AssetBase + "/" + id + "/" + digest + "/" + strings.Join(segs, "/")
}

func (r *Registry) packageInfo(p *pkgState) *PackageInfo {
	m := p.m
	inf := &PackageInfo{
		Digest:        p.digest,
		EntryURL:      r.AssetURL(m.ID, p.digest, m.Web.Entry),
		Notifications: m.Notifications,
		Translations:  m.Translations,
		HasBackend:    p.backend() != "",
		Connect:       m.Web.Connect,
	}
	if m.Icon != "" {
		inf.IconURL = r.AssetURL(m.ID, p.digest, m.Icon)
	}
	for _, k := range m.NodeKinds {
		ki := KindInfo{Kind: k.Kind, Label: k.Label, NewLabel: k.NewLabel, DefaultName: k.DefaultName, NewHint: k.NewHint}
		switch {
		case k.Icon != "":
			ki.IconURL = r.AssetURL(m.ID, p.digest, k.Icon)
		case m.Icon != "":
			ki.IconURL = inf.IconURL
		}
		inf.Kinds = append(inf.Kinds, ki)
	}
	return inf
}

// Connect is the origins an app's frame may fetch from directly, for its
// content security policy; ok is false for an id that is not installed.
func (r *Registry) Connect(id string) (origins []string, ok bool) {
	e := r.lookup(id)
	if e == nil {
		return nil, false
	}
	return e.m.Web.Connect, true
}

func (r *Registry) stateFromRow(row store.ExtPackage) (*pkgState, error) {
	m, err := apppkg.ParseManifest(row.Manifest)
	if err != nil {
		return nil, err
	}
	if m.ID != row.ExtID {
		return nil, fmt.Errorf("manifest id %q is not the row's %q", m.ID, row.ExtID)
	}
	p := &pkgState{
		m: m, digest: row.Digest, size: row.Size,
		backendURL:  row.BackendUrl.String,
		installedAt: row.InstalledAt.Time, installedBy: row.InstalledBy,
		updatedAt: row.UpdatedAt.Time, updatedBy: row.UpdatedBy,
	}
	if row.BackendSecret.Valid {
		s, err := openSecret(r.cfg.SecretKey, row.BackendSecret.String)
		if err != nil {
			// The key changed or the row was damaged: the app still opens,
			// its backend waits for a new secret.
			slog.Error("exthost: backend secret unreadable; generate a new one", "app", row.ExtID, "err", err)
		} else {
			p.secret = s
		}
	}
	return p, nil
}

// loadPackages adds the installed apps to the registry. An app this server
// can no longer read is skipped with an error in the log rather than
// refusing to start.
func (r *Registry) loadPackages(ctx context.Context) error {
	rows, err := r.cfg.Store.ListExtPackages(ctx)
	if err != nil {
		return fmt.Errorf("exthost: list packages: %w", err)
	}
	for _, row := range rows {
		p, err := r.stateFromRow(row)
		if err != nil {
			slog.Error("exthost: installed app cannot be loaded", "app", row.ExtID, "err", err)
			continue
		}
		r.put(p)
	}
	return nil
}

// Install adds an uploaded app, or replaces the installed version when
// upgrade is set. Its switches, records, backend address and secret are kept
// across an upgrade.
func (r *Registry) Install(ctx context.Context, b apppkg.Bundle, by pgtype.UUID, upgrade bool) (*PackageAdmin, error) {
	id := b.Manifest.ID
	if r.lookup(id) != nil && !upgrade {
		return nil, ErrInstalled
	}
	tx, err := r.cfg.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.cfg.Store.WithTx(tx)
	row, err := q.UpsertExtPackage(ctx, store.UpsertExtPackageParams{
		ExtID: id, Manifest: b.ManifestJSON, Version: b.Manifest.Version,
		Digest: b.Digest, Size: b.Size, By: by,
	})
	if err != nil {
		return nil, err
	}
	if err := q.DeleteExtPackageFiles(ctx, id); err != nil {
		return nil, err
	}
	files := make([]store.InsertExtPackageFilesParams, 0, len(b.Files))
	for _, f := range b.Files {
		files = append(files, store.InsertExtPackageFilesParams{
			ExtID: id, Path: f.Path, ContentType: f.ContentType, Sha256: f.SHA256, Body: f.Body,
		})
	}
	if _, err := q.InsertExtPackageFiles(ctx, files); err != nil {
		return nil, err
	}
	p, err := r.stateFromRow(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	r.put(p)
	return r.admin(p), nil
}

// Remove uninstalls an app. Its switches go with it; purge also deletes every
// record of its nodes. The nodes themselves stay, shown as items of an app
// this installation does not have, until somebody trashes them — a reinstall
// without purge brings them back whole.
func (r *Registry) Remove(ctx context.Context, id string, purge bool) error {
	if r.lookup(id) == nil {
		return ErrUnknownExt
	}
	tx, err := r.cfg.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.cfg.Store.WithTx(tx)
	if _, err := q.DeleteExtPackage(ctx, id); err != nil {
		return err
	}
	if err := q.DeleteExtStateForExt(ctx, id); err != nil {
		return err
	}
	if purge {
		if _, err := q.PurgeExtRecordsForExt(ctx, id+".%"); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	r.drop(id)
	return nil
}

// PackageAdmin is an installed app as Admin › Apps manages it.
type PackageAdmin struct {
	ID                 string
	Version            string
	Digest             string
	Size               int64
	BackendURL         string // the administrator's override
	ManifestBackendURL string
	HasSecret          bool
	InstalledAt        time.Time
	InstalledBy        pgtype.UUID
	UpdatedAt          time.Time
	UpdatedBy          pgtype.UUID
}

func (r *Registry) admin(p *pkgState) *PackageAdmin {
	a := &PackageAdmin{
		ID: p.m.ID, Version: p.m.Version, Digest: p.digest, Size: p.size,
		BackendURL: p.backendURL, HasSecret: p.secret != "",
		InstalledAt: p.installedAt, InstalledBy: p.installedBy,
		UpdatedAt: p.updatedAt, UpdatedBy: p.updatedBy,
	}
	if p.m.Backend != nil {
		a.ManifestBackendURL = p.m.Backend.URL
	}
	return a
}

// Package is the administration view of an installed app.
func (r *Registry) Package(id string) (*PackageAdmin, error) {
	e := r.lookup(id)
	if e == nil {
		return nil, ErrUnknownExt
	}
	return r.admin(e), nil
}

// ValidBackendURL checks an address for an app's backend: absolute http(s),
// no credentials, query or fragment.
func ValidBackendURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return &InvalidError{Msg: "the backend address must be an http or https URL without credentials, query or fragment"}
	}
	return nil
}

// SetBackendURL overrides the manifest's backend address; "" goes back to
// the manifest's.
func (r *Registry) SetBackendURL(ctx context.Context, id, raw string, by pgtype.UUID) (*PackageAdmin, error) {
	if raw != "" {
		if err := ValidBackendURL(raw); err != nil {
			return nil, err
		}
	}
	return r.update(id, func(p *pkgState) error {
		_, err := r.cfg.Store.SetExtPackageBackendURL(ctx, store.SetExtPackageBackendURLParams{
			ExtID: id, BackendUrl: pgtype.Text{String: raw, Valid: raw != ""}, By: by,
		})
		p.backendURL = raw
		return err
	})
}

// RotateBackendSecret generates the key backend tokens are signed with and
// returns it, once: it is stored sealed and never shown again.
func (r *Registry) RotateBackendSecret(ctx context.Context, id string, by pgtype.UUID) (string, *PackageAdmin, error) {
	if len(r.cfg.SecretKey) == 0 {
		return "", nil, ErrNoSecretKey
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	sealed, err := sealSecret(r.cfg.SecretKey, secret)
	if err != nil {
		return "", nil, err
	}
	a, err := r.update(id, func(p *pkgState) error {
		_, err := r.cfg.Store.SetExtPackageBackendSecret(ctx, store.SetExtPackageBackendSecretParams{
			ExtID: id, BackendSecret: pgtype.Text{String: sealed, Valid: true}, By: by,
		})
		p.secret = secret
		return err
	})
	if err != nil {
		return "", nil, err
	}
	return secret, a, nil
}

// update applies change to a copy of an app's state and installs the copy.
func (r *Registry) update(id string, change func(p *pkgState) error) (*PackageAdmin, error) {
	e := r.lookup(id)
	if e == nil {
		return nil, ErrUnknownExt
	}
	next := *e
	if err := change(&next); err != nil {
		return nil, err
	}
	next.updatedAt = time.Now()
	r.put(&next)
	return r.admin(&next), nil
}

// subjectPrefix names a record as the subject of a comment thread or a
// notification.
const subjectPrefix = "record:"

// recordSubject is the comment subject rule: "record:<id>" of a record the
// node holds.
func (r *Registry) recordSubject(ctx context.Context, node pgtype.UUID, subject string) (bool, error) {
	raw, ok := strings.CutPrefix(subject, subjectPrefix)
	if !ok {
		return false, nil
	}
	id, err := parseUUID(raw)
	if err != nil {
		return false, nil
	}
	_, err = r.cfg.Store.GetExtRecord(ctx, store.GetExtRecordParams{NodeID: node, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

const secretAAD = "exthost/backend-secret"

func sealSecret(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), []byte(secretAAD))), nil
}

func openSecret(key []byte, sealed string) (string, error) {
	if len(key) == 0 {
		return "", ErrNoSecretKey
	}
	data, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("sealed secret too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(secretAAD))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
