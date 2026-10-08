// Package extsdk is the contract between pp-hub and an extension: a small app
// compiled into the server, switched on per instance and per space. See
// docs/architecture/extensions.md.
//
// It sits outside internal/ on purpose: Go forbids importing another module's
// internal packages, so a package that third parties will one day import
// cannot start there. It must therefore import nothing from internal/ either;
// TestExtImportsOnlySDK holds the same line for the extensions themselves.
//
// An extension runs inside the server process, with its privileges. Including
// one in a build is the operator trusting its code; nothing here is a sandbox.
package extsdk

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Ext is one extension.
type Ext interface {
	Manifest() Manifest
	// Register wires the extension's routes and hooks. It runs once at
	// startup, after the extension's migrations have been applied.
	Register(h Host) error
}

// Manifest describes an extension to the host.
type Manifest struct {
	// ID is the extension's name in URLs, schemas and settings: lowercase
	// letters, digits and underscores, starting with a letter. Its routes are
	// /api/v1/ext/{ID}, its tables live in the Postgres schema ext_{ID}.
	ID   string
	Name string
	// Version is the extension's semver; Description, Author, License and
	// Homepage are shown in Admin › Apps. License and Homepage are optional.
	Version     string
	Description string
	Author      string
	License     string
	Homepage    string
	// SDK is the APIVersion the extension was written for (engines.sdk).
	SDK int
	// NodeKinds are the kinds of node the extension owns.
	NodeKinds []NodeKind
	// Migrations holds the extension's schema as NNNN_name.sql files at its
	// root. Each runs once, in order, in a transaction whose search_path is
	// ext_{ID}, public. Code outside migrations qualifies table names.
	Migrations fs.FS
}

// Kind returns the declared node kind named kind, as a pointer into NodeKinds
// so the extension can attach its hooks; nil when the manifest has no such kind.
func (m Manifest) Kind(kind string) *NodeKind {
	for i := range m.NodeKinds {
		if m.NodeKinds[i].Kind == kind {
			return &m.NodeKinds[i]
		}
	}
	return nil
}

// Schema is the Postgres schema the extension owns.
func (m Manifest) Schema() string { return "ext_" + m.ID }

// NodeKind is a kind of node an extension owns. It is listed under the
// extension's entry in the sidebar, not among the drive's files, and holds its
// content in the extension's tables.
type NodeKind struct {
	// Kind is "<ext id>.<name>", e.g. "kanban.board".
	Kind string
	// OnCreate seeds the extension's rows for a new node. It runs in the
	// transaction that creates the node.
	OnCreate func(ctx context.Context, tx Querier, node Node) error
	// OnCopy copies the extension's rows from src to dst, the new node. It
	// runs in the transaction that creates the copy.
	OnCopy func(ctx context.Context, tx Querier, src, dst Node) error
	// ValidSubject says whether subject names a part of node that may carry a
	// comment thread ("card:<uuid>"). Nil means the node's parts take none.
	ValidSubject func(ctx context.Context, q Querier, node Node, subject string) (bool, error)
}

// Querier is what a hook may run SQL on: the transaction of the operation.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB is the server's connection pool.
type DB interface {
	Querier
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Host is what the server offers an extension.
type Host interface {
	// Router is the extension's routes, mounted at /api/v1/ext/{ID} inside the
	// authenticated group: the session, same-origin and audit rules apply with
	// no opt-in. Requests are answered 403 while the extension is off for the
	// instance.
	Router() chi.Router
	DB() DB
	// Guard is a middleware for a route that names a node of the given kind
	// in its {nodeID} path segment. It refuses (404) anything else, refuses
	// (403) a space where the extension is off or a caller lacking need on the
	// node, and stores the node, caller and capabilities for RequestFrom.
	Guard(kind string, need Cap) func(http.Handler) http.Handler
	// Allowed reports whether user may still do need to the live node of the
	// given kind, with the extension on for its space. A long-lived stream asks
	// it to notice a revoked share, a trashed node or a switched-off app.
	Allowed(ctx context.Context, node pgtype.UUID, user pgtype.UUID, kind string, need Cap) bool
	// Caps is what user may do to node. It is the one access rule: an
	// extension maps these to its own verbs and never decides access itself.
	Caps(ctx context.Context, node Node, user pgtype.UUID) (Caps, error)
	// SearchPeople finds people who hold view on the node and match query
	// (name or email; empty lists the first few).
	SearchPeople(ctx context.Context, node Node, query string, limit int) ([]User, error)
	// Users resolves display names for ids; unknown ids are absent.
	Users(ctx context.Context, ids []pgtype.UUID) (map[pgtype.UUID]User, error)
	Realtime() Realtime
	// Resolve looks up the live nodes among ids that viewer may open, for an
	// extension that points at nodes. A node the viewer cannot open is simply
	// absent: its name must not leak through a link.
	Resolve(ctx context.Context, viewer pgtype.UUID, ids []pgtype.UUID) (map[pgtype.UUID]LinkTarget, error)
	// Comments reads and tidies the discussion on a node's parts.
	Comments() Comments
	// Notify tells a person about something on a node.
	Notify() Notify
}

// LinkTarget is a node an extension points at.
type LinkTarget struct {
	ID      pgtype.UUID
	SpaceID pgtype.UUID
	Name    string
	// Kind is "dir", "file" or "ext"; ExtKind names an extension node's kind.
	Kind    string
	ExtKind string
}

// Comments is the discussion on the parts of a node, kept by the host
// (comment threads with a subject). An extension never reads those tables.
type Comments interface {
	// Counts is the number of comments per subject on node.
	Counts(ctx context.Context, node pgtype.UUID) (map[string]int64, error)
	// Forget deletes the threads about a subject: a deleted card's discussion.
	Forget(ctx context.Context, node pgtype.UUID, subject string) error
}

// Notification is one thing to tell somebody about.
type Notification struct {
	To    pgtype.UUID
	Actor pgtype.UUID
	Node  Node
	// Verb makes the notification kind "ext:<ext id>:<verb>".
	Verb string
	// Subject is the part of the node it is about ("card:<uuid>").
	Subject string
	// Excerpt is the short text it quotes.
	Excerpt string
}

// Notify writes in-app notifications. Somebody who cannot open the node, or
// who is the actor, is not told.
type Notify interface {
	Send(ctx context.Context, n Notification) error
}

// Realtime fans events out to the people watching a topic, in this process.
type Realtime interface {
	// Publish sends an event to every open stream on topic.
	Publish(topic, event string, data any)
	// Stream serves a Server-Sent Events response on topic until the client
	// leaves. Every minute it calls stillAllowed; false ends the stream, which
	// is how a revoked share closes a board that is open.
	Stream(w http.ResponseWriter, r *http.Request, topic string, stillAllowed func(ctx context.Context) bool)
}

// Cap is one capability, ordered as the permission model orders them.
type Cap int

const (
	CapView Cap = iota
	CapDownload
	CapShare
	CapEdit
)

// Caps are the capabilities a person holds on a node.
type Caps struct {
	View     bool `json:"view"`
	Download bool `json:"download"`
	Share    bool `json:"share"`
	Edit     bool `json:"edit"`
	Owner    bool `json:"owner"`
}

// Has reports whether the set includes c.
func (c Caps) Has(want Cap) bool {
	switch want {
	case CapView:
		return c.View
	case CapDownload:
		return c.Download
	case CapShare:
		return c.Share
	case CapEdit:
		return c.Edit
	}
	return false
}

// User is a person, as an extension sees one.
type User struct {
	ID    pgtype.UUID `json:"id"`
	Name  string      `json:"name"`
	Email string      `json:"email"`
}

// Node is a node an extension owns.
type Node struct {
	ID        pgtype.UUID
	SpaceID   pgtype.UUID
	ParentID  pgtype.UUID
	Name      string
	ExtKind   string
	CreatedBy pgtype.UUID
}

// Request is what Guard established for a request.
type Request struct {
	Node   Node
	User   User
	Caps   Caps
	Method string
}

type requestKey struct{}

// WithRequest stores the guarded request's facts. The host's Guard calls it.
func WithRequest(ctx context.Context, rq Request) context.Context {
	return context.WithValue(ctx, requestKey{}, rq)
}

// RequestFrom returns what Guard stored; ok is false on a route that has no
// Guard in front of it.
func RequestFrom(ctx context.Context) (Request, bool) {
	rq, ok := ctx.Value(requestKey{}).(Request)
	return rq, ok
}

// UUIDString is the canonical text of an id, or "" when it is not set.
func UUIDString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	v, _ := id.Value()
	s, _ := v.(string)
	return s
}

// ParseUUID reads an id from text.
func ParseUUID(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	err := id.Scan(s)
	return id, err
}

// WriteJSON answers status with v as JSON.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteProblem answers with an RFC 7807 problem, as the rest of the API does.
func WriteProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   "about:blank",
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
	})
}

// MaxBodyBytes bounds a JSON request body read through DecodeBody.
const MaxBodyBytes = 1 << 20

// DecodeBody reads a JSON body into v, answering 400 itself on failure.
func DecodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		WriteProblem(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
