// Package exthost runs apps (M44, reshaped by M51): small programs an
// administrator uploads as a package, switched on per instance and per space.
// Design in docs/architecture/extensions.md.
//
// No app code runs in this process. An app's UI is static files shown in a
// sandboxed frame (packages.go serves them through httpapi); what it stores
// lives in the host's record store, guarded by the node's capabilities
// through share's resolver (apps.go); server logic, when an app has any, runs
// in its author's own service, reached through a signed proxy (backend.go).
package exthost

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var (
	// ErrUnknownKind is a node kind no installed app owns.
	ErrUnknownKind = errors.New("unknown app node kind")
	// ErrUnknownExt is an app id that is not installed.
	ErrUnknownExt = errors.New("unknown app")
	// ErrDisabled is an app that is switched off where it was used.
	ErrDisabled = errors.New("this app is turned off")
)

// Config is what the host needs from the server.
type Config struct {
	Pool  *pgxpool.Pool
	Store *store.Queries
	FS    *fs.Service
	Share *share.Service
	// Notifications delivers an app's in-app notifications.
	Notifications *notifications.Service
	// StreamRecheck is how often an open stream re-asks whether its viewer
	// may still watch; zero is a minute.
	StreamRecheck time.Duration
	// SecretKey seals apps' backend secrets at rest
	// (config.DeriveKey("exthost/backend-secret")). Without one a backend
	// secret cannot be generated.
	SecretKey []byte
	// ExternalURL is PP_EXTERNAL_URL: the issuer of backend tokens.
	ExternalURL string
	// AssetBase is where apps' files are served from, without a trailing
	// slash: PP_ARTIFACT_URL or PP_EXTERNAL_URL, plus AssetPath.
	AssetBase string
	// BackendClient calls apps' backends; nil is a client with
	// BackendTimeout that does not follow redirects.
	BackendClient *http.Client
}

func (r *Registry) recheck() time.Duration {
	if r.cfg.StreamRecheck > 0 {
		return r.cfg.StreamRecheck
	}
	return defaultRecheck
}

// Registry holds the apps installed on this instance. They come and go at
// runtime, so every read of the maps holds mu.
type Registry struct {
	cfg           Config
	caller        func(*http.Request) store.User
	sessionActive func(*http.Request) bool
	mu            sync.RWMutex
	apps          []*entry
	byID          map[string]*entry
	kinds         map[string]*entry
	rt            *broker
}

// entry is an installed app. It is replaced whole on every change, never
// mutated, so a request may keep the pointer it read under the lock.
type entry = pkgState

// New makes an empty registry. Start loads the installed apps.
func New(cfg Config) *Registry {
	return &Registry{
		cfg:   cfg,
		byID:  map[string]*entry{},
		kinds: map[string]*entry{},
		rt:    newBroker(),
	}
}

// Bind gives the host what only the HTTP edge knows: the person behind a
// request in the authenticated group. httpapi.NewServer calls it before the
// first request.
//
// sessionActive reports whether a request's session still stands; a stream
// asks it every minute so signing out ends it.
func (r *Registry) Bind(caller func(*http.Request) store.User, sessionActive func(*http.Request) bool) {
	r.caller = caller
	r.sessionActive = sessionActive
}

// Start loads the installed apps.
func (r *Registry) Start(ctx context.Context) error { return r.loadPackages(ctx) }

// Close ends every open stream; the server is shutting down.
func (r *Registry) Close() { r.rt.close() }

// Mount adds the runtime every app shares, under /apps/{id} of the API router.
func (r *Registry) Mount(api chi.Router) { r.mountApps(api) }

// lookup is the app with id, or nil.
func (r *Registry) lookup(id string) *entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[id]
}

// kindOf is the app owning a node kind, or nil.
func (r *Registry) kindOf(kind string) *entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.kinds[kind]
}

// entries is a snapshot of every app, ordered by id.
func (r *Registry) entries() []*entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]*entry(nil), r.apps...)
}

// put installs or replaces an app's entry.
func (r *Registry) put(e *entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dropLocked(e.m.ID)
	for _, k := range e.m.NodeKinds {
		r.kinds[k.Kind] = e
	}
	r.byID[e.m.ID] = e
	r.apps = append(r.apps, e)
	sort.Slice(r.apps, func(i, j int) bool { return r.apps[i].m.ID < r.apps[j].m.ID })
}

func (r *Registry) drop(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dropLocked(id)
}

func (r *Registry) dropLocked(id string) {
	old := r.byID[id]
	if old == nil {
		return
	}
	for _, k := range old.m.NodeKinds {
		delete(r.kinds, k.Kind)
	}
	delete(r.byID, id)
	for i, x := range r.apps {
		if x == old {
			r.apps = append(r.apps[:i], r.apps[i+1:]...)
			break
		}
	}
}

// Enabled reports whether an app is on for a space: the instance switch is
// on and the space has not switched it off. Unknown apps are off.
func (r *Registry) Enabled(ctx context.Context, extID string, spaceID pgtype.UUID) (bool, error) {
	if r.lookup(extID) == nil {
		return false, nil
	}
	on, err := r.cfg.Store.ExtEnabledForSpace(ctx, store.ExtEnabledForSpaceParams{ExtID: extID, SpaceID: spaceID})
	return on.Valid && on.Bool, err
}

// KindOwner returns the app that owns a node kind.
func (r *Registry) KindOwner(kind string) (extID string, ok bool) {
	e := r.kindOf(kind)
	if e == nil {
		return "", false
	}
	return e.m.ID, true
}

// Info describes an app to the settings screens and the SPA.
type Info struct {
	ID          string
	Name        string
	Version     string
	Description string
	Author      string
	License     string
	Homepage    string
	NodeKinds   []string
	// Instance is the instance switch; Space is whether the space has it on
	// for itself, only when a space was asked about (a space that never chose
	// follows the instance), and Enabled the effective answer.
	Instance bool
	Space    bool
	Enabled  bool
	// Package is what the SPA needs to draw and open the app.
	Package *PackageInfo
}

// List describes every installed app. spaceID, when valid, adds the space's
// own state.
func (r *Registry) List(ctx context.Context, spaceID pgtype.UUID) ([]Info, error) {
	rows, err := r.cfg.Store.ListExtState(ctx)
	if err != nil {
		return nil, err
	}
	instance := map[string]bool{}
	spaceOff := map[string]bool{}
	for _, row := range rows {
		switch {
		case !row.SpaceID.Valid:
			instance[row.ExtID] = row.Enabled
		case spaceID.Valid && row.SpaceID == spaceID && !row.Enabled:
			spaceOff[row.ExtID] = true
		}
	}
	all := r.entries()
	out := make([]Info, 0, len(all))
	for _, e := range all {
		m := e.m
		inf := Info{
			ID: m.ID, Name: m.Name, Version: m.Version, Description: m.Description,
			Author: m.Author, License: m.License, Homepage: m.Homepage,
			Instance: instance[m.ID], Package: r.packageInfo(e),
		}
		for _, k := range m.NodeKinds {
			inf.NodeKinds = append(inf.NodeKinds, k.Kind)
		}
		inf.Space = inf.Instance && !spaceOff[m.ID]
		inf.Enabled = inf.Space
		out = append(out, inf)
	}
	return out, nil
}

// Known reports whether the app is installed.
func (r *Registry) Known(extID string) bool { return r.lookup(extID) != nil }

// SetInstance flips the instance switch.
func (r *Registry) SetInstance(ctx context.Context, extID string, enabled bool, by pgtype.UUID) error {
	if !r.Known(extID) {
		return ErrUnknownExt
	}
	return r.cfg.Store.SetExtInstanceState(ctx, store.SetExtInstanceStateParams{ExtID: extID, Enabled: enabled, UpdatedBy: by})
}

// SetSpace flips a space's own switch. Turning it back on clears the row, so
// the space follows the instance again.
func (r *Registry) SetSpace(ctx context.Context, extID string, spaceID pgtype.UUID, enabled bool, by pgtype.UUID) error {
	if !r.Known(extID) {
		return ErrUnknownExt
	}
	if enabled {
		return r.cfg.Store.ClearExtSpaceState(ctx, store.ClearExtSpaceStateParams{ExtID: extID, SpaceID: spaceID})
	}
	return r.cfg.Store.SetExtSpaceState(ctx, store.SetExtSpaceStateParams{ExtID: extID, SpaceID: spaceID, Enabled: false, UpdatedBy: by})
}

// OnCreate is fs.ExtHooks: it writes a new node's starter records (the
// kind's seed), in the node's transaction.
func (r *Registry) OnCreate(ctx context.Context, tx store.DBTX, node store.Node) error {
	e := r.kindOf(node.ExtKind.String)
	if e == nil {
		return ErrUnknownKind
	}
	q := store.New(tx)
	for _, k := range e.m.NodeKinds {
		if k.Kind != node.ExtKind.String {
			continue
		}
		for _, s := range k.Seed {
			if _, err := q.CreateExtRecord(ctx, store.CreateExtRecordParams{
				NodeID: node.ID, Collection: s.Collection, Data: s.Data, By: node.CreatedBy,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidSubject is the comment subject rule: "record:<id>" of a record the
// node holds.
func (r *Registry) ValidSubject(ctx context.Context, node store.Node, subject string) (bool, error) {
	if r.kindOf(node.ExtKind.String) == nil {
		return false, nil
	}
	return r.recordSubject(ctx, node.ID, subject)
}

// OnCopy is fs.ExtHooks: a copy of a node holds its records under the same
// ids, so references between them survive. An item of an app that is not
// installed cannot be copied: nothing would open the copy.
func (r *Registry) OnCopy(ctx context.Context, tx store.DBTX, src, dst store.Node) error {
	if r.kindOf(src.ExtKind.String) == nil {
		return ErrUnknownKind
	}
	return store.New(tx).CopyExtRecords(ctx, store.CopyExtRecordsParams{Src: src.ID, Dst: dst.ID})
}
