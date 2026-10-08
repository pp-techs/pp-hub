// Package exthost runs extensions (M44): it validates their manifests, applies
// their migrations, hands each one a narrow facade of the server, mounts their
// routes in the authenticated API and answers whether one is switched on.
// Design in docs/architecture/extensions.md.
//
// An extension never receives the store, a module or a request's session. It
// gets extsdk.Host, whose every access decision goes through share's
// resolver, so no access rule exists twice.
package exthost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var idRe = regexp.MustCompile(extsdk.IDPattern)

var (
	// ErrUnknownKind is a node kind no extension in this build owns.
	ErrUnknownKind = errors.New("unknown extension node kind")
	// ErrUnknownExt is an extension id this build does not carry.
	ErrUnknownExt = errors.New("unknown extension")
	// ErrDisabled is an extension that is switched off where it was used.
	ErrDisabled = errors.New("this app is turned off")
)

// Config is what the host needs from the server.
type Config struct {
	Pool  *pgxpool.Pool
	Store *store.Queries
	FS    *fs.Service
	Share *share.Service
	// Notifications delivers an extension's in-app notifications.
	Notifications *notifications.Service
	// StreamRecheck is how often an open stream re-asks whether its viewer
	// may still watch; zero is a minute.
	StreamRecheck time.Duration
}

func (r *Registry) recheck() time.Duration {
	if r.cfg.StreamRecheck > 0 {
		return r.cfg.StreamRecheck
	}
	return defaultRecheck
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

// Registry holds the extensions of this build.
type Registry struct {
	cfg           Config
	caller        func(*http.Request) store.User
	sessionActive func(*http.Request) bool
	exts          []*entry
	byID          map[string]*entry
	kinds         map[string]kindEntry
	rt            *broker
}

type entry struct {
	ext    extsdk.Ext
	m      extsdk.Manifest
	router chi.Router
}

type kindEntry struct {
	e    *entry
	kind extsdk.NodeKind
}

// New validates the extensions' manifests. Nothing runs until Start.
func New(cfg Config, exts ...extsdk.Ext) (*Registry, error) {
	r := &Registry{
		cfg:   cfg,
		byID:  map[string]*entry{},
		kinds: map[string]kindEntry{},
		rt:    newBroker(),
	}
	for _, x := range exts {
		m := x.Manifest()
		if m.SDK != extsdk.APIVersion {
			return nil, fmt.Errorf("exthost: extension %q was written for extension API %d, this server provides %d", m.ID, m.SDK, extsdk.APIVersion)
		}
		if !idRe.MatchString(m.ID) {
			return nil, fmt.Errorf("exthost: extension id %q must match %s", m.ID, extsdk.IDPattern)
		}
		if m.Name == "" || m.Version == "" {
			return nil, fmt.Errorf("exthost: extension %q has an invalid manifest: name and version are required", m.ID)
		}
		if r.byID[m.ID] != nil {
			return nil, fmt.Errorf("exthost: extension %q registered twice", m.ID)
		}
		e := &entry{ext: x, m: m, router: chi.NewRouter()}
		for _, k := range m.NodeKinds {
			if !strings.HasPrefix(k.Kind, m.ID+".") || len(k.Kind) == len(m.ID)+1 {
				return nil, fmt.Errorf("exthost: node kind %q of %q must be %q<name>", k.Kind, m.ID, m.ID+".")
			}
			if _, dup := r.kinds[k.Kind]; dup {
				return nil, fmt.Errorf("exthost: node kind %q registered twice", k.Kind)
			}
			r.kinds[k.Kind] = kindEntry{e: e, kind: k}
		}
		r.byID[m.ID] = e
		r.exts = append(r.exts, e)
	}
	sort.Slice(r.exts, func(i, j int) bool { return r.exts[i].m.ID < r.exts[j].m.ID })
	return r, nil
}

// Start applies every extension's migrations, then lets each register. Only the
// process that owns migrations calls it before serving.
func (r *Registry) Start(ctx context.Context) error {
	if err := r.migrate(ctx); err != nil {
		return err
	}
	return r.Register()
}

// Register lets each extension add its routes and hooks. Start calls it after
// the migrations; it is separate so a test can inspect the routes with no
// database.
func (r *Registry) Register() error {
	for _, e := range r.exts {
		if err := e.ext.Register(&extHost{r: r, e: e}); err != nil {
			return fmt.Errorf("exthost: register %s: %w", e.m.ID, err)
		}
	}
	return nil
}

// Close ends every open stream; the server is shutting down.
func (r *Registry) Close() { r.rt.close() }

// Mount adds every extension's routes under /ext/{id} of the API router.
func (r *Registry) Mount(api chi.Router) {
	for _, e := range r.exts {
		e := e
		api.Route("/ext/"+e.m.ID, func(sub chi.Router) {
			sub.Use(r.instanceGate(e.m.ID))
			sub.Mount("/", e.router)
		})
	}
}

func (r *Registry) instanceGate(id string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			on, err := r.cfg.Store.ExtEnabledForInstance(req.Context(), id)
			if err != nil {
				extsdk.WriteProblem(w, http.StatusInternalServerError, "")
				return
			}
			if !on {
				extsdk.WriteProblem(w, http.StatusForbidden, ErrDisabled.Error())
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

// Enabled reports whether an extension is on for a space: the instance switch
// is on and the space has not switched it off. Unknown extensions are off.
func (r *Registry) Enabled(ctx context.Context, extID string, spaceID pgtype.UUID) (bool, error) {
	if r.byID[extID] == nil {
		return false, nil
	}
	on, err := r.cfg.Store.ExtEnabledForSpace(ctx, store.ExtEnabledForSpaceParams{ExtID: extID, SpaceID: spaceID})
	return on.Valid && on.Bool, err
}

// KindOwner returns the extension that owns a node kind.
func (r *Registry) KindOwner(kind string) (extID string, ok bool) {
	k, ok := r.kinds[kind]
	if !ok {
		return "", false
	}
	return k.e.m.ID, true
}

// Info describes an extension to the settings screens.
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
}

// List describes every extension of the build. spaceID, when valid, adds the
// space's own state.
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
	out := make([]Info, 0, len(r.exts))
	for _, e := range r.exts {
		inf := Info{
			ID: e.m.ID, Name: e.m.Name, Version: e.m.Version, Description: e.m.Description,
			Author: e.m.Author, License: e.m.License, Homepage: e.m.Homepage,
			Instance: instance[e.m.ID],
		}
		for _, k := range e.m.NodeKinds {
			inf.NodeKinds = append(inf.NodeKinds, k.Kind)
		}
		inf.Space = inf.Instance && !spaceOff[e.m.ID]
		inf.Enabled = inf.Space
		out = append(out, inf)
	}
	return out, nil
}

// Known reports whether the build carries the extension.
func (r *Registry) Known(extID string) bool { return r.byID[extID] != nil }

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

// OnCreate is fs.ExtHooks: it seeds a new node through its extension.
func (r *Registry) OnCreate(ctx context.Context, tx store.DBTX, node store.Node) error {
	k, ok := r.kinds[node.ExtKind.String]
	if !ok {
		return ErrUnknownKind
	}
	if k.kind.OnCreate == nil {
		return nil
	}
	return k.kind.OnCreate(ctx, tx, toNode(node))
}

// ValidSubject asks the node's extension whether subject names a part of the
// node that may carry a comment thread.
func (r *Registry) ValidSubject(ctx context.Context, node store.Node, subject string) (bool, error) {
	k, ok := r.kinds[node.ExtKind.String]
	if !ok || k.kind.ValidSubject == nil {
		return false, nil
	}
	return k.kind.ValidSubject(ctx, r.cfg.Pool, toNode(node), subject)
}

// OnCopy is fs.ExtHooks: it copies a node's rows through its extension.
func (r *Registry) OnCopy(ctx context.Context, tx store.DBTX, src, dst store.Node) error {
	k, ok := r.kinds[src.ExtKind.String]
	if !ok {
		return ErrUnknownKind
	}
	if k.kind.OnCopy == nil {
		return nil
	}
	return k.kind.OnCopy(ctx, tx, toNode(src), toNode(dst))
}

func toNode(n store.Node) extsdk.Node {
	return extsdk.Node{
		ID: n.ID, SpaceID: n.SpaceID, ParentID: n.ParentID, Name: n.Name,
		ExtKind: n.ExtKind.String, CreatedBy: n.CreatedBy,
	}
}
