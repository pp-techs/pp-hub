// Package publish turns an .html file or a folder into a public mini site
// (M50): a snapshot of the source's files, served under a sandboxing CSP from
// a second origin (PP_ARTIFACT_URL) or, without one, from this origin under
// SamePath. See
// docs/architecture/publishing.md.
//
// This package owns the model — snapshots, tokens, passwords, grants and the
// liveness rules a request re-checks. The HTTP surfaces (the management
// routes, the /p viewer API and the artifact host) live in internal/httpapi.
package publish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Key purposes for config.DeriveKey. Owned here, used by nothing else.
const (
	TokenKeyPurpose = "publication-token-v1"
	GrantKeyPurpose = "publication-grant-v1"
)

// MaxFiles bounds one snapshot's file count.
const MaxFiles = 2000

var (
	// ErrNotFound is every reason a publication is not served: no such
	// token, expired, switched off, or its creator gone — one answer, so a
	// visitor cannot tell them apart.
	ErrNotFound = errors.New("publication not found")
	// ErrNotPublishable refuses a source that is neither an HTML file nor a
	// folder.
	ErrNotPublishable = errors.New("only an .html file or a folder can be published")
	// ErrNoEntry refuses a folder with no index.html when no entry_path
	// names another page.
	ErrNoEntry = errors.New("this folder has no index.html: choose the page it opens on (entry_path)")
	// ErrBadEntry refuses an entry_path that is not a file in the snapshot.
	ErrBadEntry = errors.New("entry_path is not a file in what would be published")
	// ErrTooManyFiles and ErrTooLarge are the snapshot limits (413).
	ErrTooManyFiles = fmt.Errorf("a publication holds at most %d files", MaxFiles)
	ErrTooLarge     = errors.New("this is larger than a publication may be")
	// ErrQuota refuses a snapshot the space has no room for (413).
	ErrQuota = errors.New("publishing this would exceed the space's storage limit")
	// ErrWrongPassword is a failed unlock.
	ErrWrongPassword = errors.New("wrong password")
	// ErrBadPassword refuses an empty password on set.
	ErrBadPassword = errors.New("a password cannot be empty")
)

// Service is the publishing model for one process.
type Service struct {
	q        *store.Queries
	blobs    blob.Store
	tokenKey []byte
	grantKey []byte
	// artifactURL is where published bytes are served from: PP_ARTIFACT_URL
	// when one is configured (isolated), otherwise PP_EXTERNAL_URL + SamePath.
	artifactURL string
	isolated    bool
	maxBytes    int64
	// switchedOn is sysconfig's artifacts_enabled.
	switchedOn func() bool
	now        func() time.Time
}

// Config is what New needs.
type Config struct {
	Store    *store.Queries
	Blobs    blob.Store
	TokenKey []byte
	GrantKey []byte
	// ArtifactURL is PP_ARTIFACT_URL; empty serves published sites on
	// ExternalURL under SamePath.
	ArtifactURL string
	ExternalURL string
	MaxBytes    int64
	// Enabled reports the instance switch (sysconfig artifacts_enabled).
	Enabled func() bool
}

// SamePath is the path prefix published sites are served under on the app's
// own origin when no artifact origin is configured.
const SamePath = "/a"

// New builds the service.
func New(c Config) *Service {
	s := &Service{
		q: c.Store, blobs: c.Blobs,
		tokenKey: c.TokenKey, grantKey: c.GrantKey,
		maxBytes:   c.MaxBytes,
		switchedOn: c.Enabled,
		now:        time.Now,
	}
	if art := strings.TrimRight(c.ArtifactURL, "/"); art != "" {
		s.artifactURL, s.isolated = art, true
	} else if ext := strings.TrimRight(c.ExternalURL, "/"); ext != "" {
		s.artifactURL = ext + SamePath
	}
	return s
}

// Enabled reports whether publishing is offered: there is somewhere to serve
// from and the instance switch is on.
func (s *Service) Enabled() bool {
	return s != nil && s.artifactURL != "" && s.switchedOn != nil && s.switchedOn()
}

// ArtifactURL is the base every content URL starts with: the artifact origin,
// or the app's origin plus SamePath. Empty when neither is known.
func (s *Service) ArtifactURL() string {
	if s == nil {
		return ""
	}
	return s.artifactURL
}

// Isolated reports whether published sites have an origin of their own
// (PP_ARTIFACT_URL). Without one the sandboxing CSP is the only thing between
// a published page and the app's origin.
func (s *Service) Isolated() bool {
	return s != nil && s.isolated
}

// File is one entry of a snapshot.
type File struct {
	Path     string
	BlobHash string
	Size     int64
	Mime     string
}

// Snapshot is what publishing a source would serve.
type Snapshot struct {
	Files []File
	Entry string
	Bytes int64
}

// IsHTML reports whether a name is an .html/.htm page.
func IsHTML(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".html" || ext == ".htm"
}

// sourceFiles lists what the source holds now, without applying limits.
func (s *Service) sourceFiles(ctx context.Context, q *store.Queries, node store.Node) ([]File, error) {
	switch node.Kind {
	case store.NodeKindFile:
		ver, err := q.GetCurrentVersion(ctx, node.ID)
		if err != nil {
			return nil, err
		}
		return []File{{Path: node.Name, BlobHash: ver.BlobHash, Size: ver.Size, Mime: ver.Mime}}, nil
	case store.NodeKindDir:
		rows, err := q.ListPublishableFiles(ctx, node.ID)
		if err != nil {
			return nil, err
		}
		files := make([]File, len(rows))
		for i, r := range rows {
			files[i] = File{Path: r.Path, BlobHash: r.BlobHash, Size: r.Size, Mime: r.Mime}
		}
		return files, nil
	}
	return nil, ErrNotPublishable
}

// TakeSnapshot reads the source's live files and resolves the entry: a file
// source is its own entry; a folder's is entryPath when given, else
// index.html at its root.
func (s *Service) TakeSnapshot(ctx context.Context, node store.Node, entryPath string) (Snapshot, error) {
	return s.takeSnapshot(ctx, s.q, node, entryPath)
}

func (s *Service) takeSnapshot(ctx context.Context, q *store.Queries, node store.Node, entryPath string) (Snapshot, error) {
	if node.Kind == store.NodeKindFile && !IsHTML(node.Name) {
		return Snapshot{}, ErrNotPublishable
	}
	files, err := s.sourceFiles(ctx, q, node)
	if err != nil {
		return Snapshot{}, err
	}
	if len(files) > MaxFiles {
		return Snapshot{}, ErrTooManyFiles
	}
	snap := Snapshot{Files: files}
	have := make(map[string]bool, len(files))
	for _, f := range files {
		snap.Bytes += f.Size
		have[f.Path] = true
	}
	if s.maxBytes > 0 && snap.Bytes > s.maxBytes {
		return Snapshot{}, ErrTooLarge
	}
	switch {
	case node.Kind == store.NodeKindFile:
		snap.Entry = node.Name
	case entryPath != "":
		p, dir, ok := CleanPath(entryPath)
		if !ok || dir || !have[p] {
			return Snapshot{}, ErrBadEntry
		}
		snap.Entry = p
	case have["index.html"]:
		snap.Entry = "index.html"
	default:
		return Snapshot{}, ErrNoEntry
	}
	return snap, nil
}

// Patch is a settings change. A nil field keeps the current value; Set with
// a nil value clears it.
type Patch struct {
	Password  *Optional[string]
	ExpiresAt *Optional[time.Time]
}

// Optional is a value that may be explicitly absent (JSON null).
type Optional[T any] struct {
	Value *T
}

// Publish takes a snapshot of node and publishes it, or replaces the
// manifest of its existing publication and bumps rev — the URL stays.
// created reports a new publication. patch applies in the same transaction.
func (s *Service) Publish(ctx context.Context, node store.Node, creator pgtype.UUID, entryPath string, patch Patch) (pub store.Publication, created bool, err error) {
	// One retry: two first publishes racing on node_id leave one to insert
	// and the other to find its row and republish.
	for range 2 {
		pub, created, err = s.publishOnce(ctx, node, creator, entryPath, patch)
		if !isUniqueViolation(err) {
			return pub, created, err
		}
	}
	return pub, created, err
}

func (s *Service) publishOnce(ctx context.Context, node store.Node, creator pgtype.UUID, entryPath string, patch Patch) (pub store.Publication, created bool, err error) {
	pw, err := s.passwordValue(patch.Password)
	if err != nil {
		return store.Publication{}, false, err
	}
	err = s.q.InTx(ctx, func(q *store.Queries) error {
		snap, err := s.takeSnapshot(ctx, q, node, entryPath)
		if err != nil {
			return err
		}
		existing, err := q.LockPublicationByNode(ctx, node.ID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			created = true
		case err != nil:
			return err
		}
		var released int64
		if !created {
			tot, err := q.PublicationTotals(ctx, existing.ID)
			if err != nil {
				return err
			}
			released = tot.TotalBytes
		}
		if err := ensureRoom(ctx, q, node.SpaceID, snap.Bytes-released); err != nil {
			return err
		}
		if created {
			token, err := share.NewLinkToken()
			if err != nil {
				return err
			}
			sealed, err := seal(s.tokenKey, token)
			if err != nil {
				return err
			}
			p := store.CreatePublicationParams{
				SpaceID: node.SpaceID, NodeID: node.ID, Title: node.Name, EntryPath: snap.Entry,
				TokenHash: hashToken(token), TokenSealed: sealed, CreatedBy: creator,
			}
			if pw != nil {
				p.PasswordHash = *pw
			}
			if patch.ExpiresAt != nil {
				p.ExpiresAt = timestamptz(patch.ExpiresAt.Value)
			}
			if pub, err = q.CreatePublication(ctx, p); err != nil {
				return err
			}
		} else {
			if err := q.DeletePublicationFiles(ctx, existing.ID); err != nil {
				return err
			}
			if pub, err = q.RepublishPublication(ctx, store.RepublishPublicationParams{
				ID: existing.ID, Title: node.Name, EntryPath: snap.Entry,
			}); err != nil {
				return err
			}
			if pw != nil || patch.ExpiresAt != nil {
				if pub, err = applySettings(ctx, q, pub, pw, patch.ExpiresAt); err != nil {
					return err
				}
			}
		}
		return insertFiles(ctx, q, pub.ID, snap.Files)
	})
	return pub, created, err
}

// UpdateSettings changes password and expiry, taking no snapshot.
func (s *Service) UpdateSettings(ctx context.Context, pub store.Publication, patch Patch) (store.Publication, error) {
	pw, err := s.passwordValue(patch.Password)
	if err != nil {
		return store.Publication{}, err
	}
	if pw == nil && patch.ExpiresAt == nil {
		return pub, nil
	}
	return applySettings(ctx, s.q, pub, pw, patch.ExpiresAt)
}

func applySettings(ctx context.Context, q *store.Queries, pub store.Publication, pw *pgtype.Text, exp *Optional[time.Time]) (store.Publication, error) {
	p := store.UpdatePublicationSettingsParams{ID: pub.ID, PasswordHash: pub.PasswordHash, ExpiresAt: pub.ExpiresAt}
	if pw != nil {
		p.PasswordHash = *pw
	}
	if exp != nil {
		p.ExpiresAt = timestamptz(exp.Value)
	}
	return q.UpdatePublicationSettings(ctx, p)
}

// passwordValue hashes a password patch: nil keeps, a NULL clears.
func (s *Service) passwordValue(o *Optional[string]) (*pgtype.Text, error) {
	if o == nil {
		return nil, nil
	}
	if o.Value == nil {
		return &pgtype.Text{}, nil
	}
	if *o.Value == "" {
		return nil, ErrBadPassword
	}
	h, err := hashPassword(*o.Value)
	if err != nil {
		return nil, err
	}
	return &pgtype.Text{String: h, Valid: true}, nil
}

func timestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func insertFiles(ctx context.Context, q *store.Queries, pubID pgtype.UUID, files []File) error {
	if len(files) == 0 {
		return nil
	}
	p := store.InsertPublicationFilesParams{
		PublicationID: pubID,
		Paths:         make([]string, len(files)),
		BlobHashes:    make([]string, len(files)),
		Sizes:         make([]int64, len(files)),
		Mimes:         make([]string, len(files)),
	}
	for i, f := range files {
		p.Paths[i], p.BlobHashes[i], p.Sizes[i], p.Mimes[i] = f.Path, f.BlobHash, f.Size, f.Mime
	}
	return q.InsertPublicationFiles(ctx, p)
}

// ensureRoom refuses a snapshot that would push the space past its quota,
// as fs.EnsureRoom refuses an upload. delta may be negative (a smaller
// republish), which always fits.
func ensureRoom(ctx context.Context, q *store.Queries, spaceID pgtype.UUID, delta int64) error {
	if delta <= 0 {
		return nil
	}
	sp, err := q.GetSpace(ctx, spaceID)
	if err != nil {
		return err
	}
	if sp.QuotaBytes > 0 && sp.UsedBytes+delta > sp.QuotaBytes {
		return ErrQuota
	}
	return nil
}

// ForNode is the node's publication, or ErrNotFound.
func (s *Service) ForNode(ctx context.Context, nodeID pgtype.UUID) (store.Publication, error) {
	pub, err := s.q.GetPublicationByNode(ctx, nodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Publication{}, ErrNotFound
	}
	return pub, err
}

// Unpublish deletes the publication; the triggers release its blobs and its
// bytes.
func (s *Service) Unpublish(ctx context.Context, pub store.Publication) error {
	n, err := s.q.DeletePublication(ctx, pub.ID)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SweepExpired deletes publications past their expiry.
func (s *Service) SweepExpired(ctx context.Context) (int64, error) {
	return s.q.DeleteExpiredPublications(ctx)
}

// Token opens the sealed token of a publication.
func (s *Service) Token(pub store.Publication) (string, error) {
	return open(s.tokenKey, pub.TokenSealed)
}

// Summary is what the management API reports beside the row.
type Summary struct {
	FileCount  int64
	TotalBytes int64
	// Stale is true when the source's files differ from the snapshot:
	// content changed, or a file was added, removed or renamed.
	Stale bool
}

// Summarize counts the snapshot and compares it with the source now.
func (s *Service) Summarize(ctx context.Context, pub store.Publication, node store.Node) (Summary, error) {
	have, err := s.q.ListPublicationFiles(ctx, pub.ID)
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{FileCount: int64(len(have))}
	snap := make(map[string]string, len(have))
	for _, f := range have {
		sum.TotalBytes += f.Size
		snap[f.Path] = f.BlobHash
	}
	now, err := s.sourceFiles(ctx, s.q, node)
	if err != nil {
		// A source that can no longer be read (trashed) is not what was
		// published.
		if errors.Is(err, pgx.ErrNoRows) {
			sum.Stale = true
			return sum, nil
		}
		return Summary{}, err
	}
	if len(now) != len(snap) {
		sum.Stale = true
		return sum, nil
	}
	for _, f := range now {
		if snap[f.Path] != f.BlobHash {
			sum.Stale = true
			break
		}
	}
	return sum, nil
}

// Served is a publication that passed every serve-time check.
type Served struct {
	Publication store.Publication
	CreatorName string
}

// NeedsPassword reports whether the content is behind a password.
func (sv Served) NeedsPassword() bool { return sv.Publication.PasswordHash.Valid }

// Resolve finds a live publication by token. Every request runs it: not
// expired, the instance offering publishing, the space's public links on
// and the space not archived, the creator still a member. Any failure is
// ErrNotFound.
func (s *Service) Resolve(ctx context.Context, token string) (Served, error) {
	if !s.Enabled() || token == "" {
		return Served{}, ErrNotFound
	}
	row, err := s.q.GetServedPublication(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return Served{}, ErrNotFound
	}
	if err != nil {
		return Served{}, err
	}
	p := row.Publication
	if p.ExpiresAt.Valid && !p.ExpiresAt.Time.After(s.now()) {
		return Served{}, ErrNotFound
	}
	if !row.AllowPublicLinks || row.SpaceArchived || !row.CreatorIsMember {
		return Served{}, ErrNotFound
	}
	return Served{Publication: p, CreatorName: row.CreatorName}, nil
}

// Unlock checks a password and mints a grant for the publication's rev.
func (s *Service) Unlock(sv Served, password string) (string, error) {
	if !sv.NeedsPassword() || !checkPassword(sv.Publication.PasswordHash.String, password) {
		return "", ErrWrongPassword
	}
	return s.mintGrant(sv.Publication, s.now().Add(GrantTTL)), nil
}

// ContentURL is where the viewer's frame points: the entry page on the
// artifact origin, with the grant segment when there is one.
func (s *Service) ContentURL(token, grant, entry string) string {
	u := s.artifactURL + "/" + token + "/"
	if grant != "" {
		u += "~" + grant + "/"
	}
	return u + escapePath(entry)
}

func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		segs[i] = urlPathEscape(seg)
	}
	return strings.Join(segs, "/")
}

// Open reads one file of a served publication. ErrNotFound when the path is
// not in the snapshot.
func (s *Service) Open(ctx context.Context, sv Served, p string) (store.PublicationFile, io.ReadCloser, error) {
	f, err := s.q.GetPublicationFile(ctx, store.GetPublicationFileParams{PublicationID: sv.Publication.ID, Path: p})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.PublicationFile{}, nil, ErrNotFound
	}
	if err != nil {
		return store.PublicationFile{}, nil, err
	}
	rc, err := s.blobs.Get(ctx, f.BlobHash)
	if err != nil {
		return store.PublicationFile{}, nil, err
	}
	return f, rc, nil
}

// ContentType is the type a file is served as: by its extension, falling
// back to the type recorded at snapshot time.
func ContentType(f store.PublicationFile) string {
	if t := mime.TypeByExtension(path.Ext(f.Path)); t != "" {
		return t
	}
	if f.Mime != "" {
		return f.Mime
	}
	return "application/octet-stream"
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}
