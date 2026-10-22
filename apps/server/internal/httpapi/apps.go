package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"

	"github.com/nhatbui/pp-hub/apps/server/internal/apppkg"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/exthost"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Packaged apps (M51): the administration routes that install, upgrade,
// configure and remove them, and the handler that serves their files to the
// sandboxed frame. The runtime the frame talks to is exthost's (apps.go).

// packageDTO is a packaged app as the SPA needs it to show and open the app;
// it rides on extensionDTO.
type packageDTO struct {
	Digest        string                       `json:"digest"`
	EntryURL      string                       `json:"entry_url"`
	IconURL       string                       `json:"icon_url"`
	Kinds         []packageKindDTO             `json:"kinds"`
	Notifications map[string]string            `json:"notifications"`
	Translations  map[string]map[string]string `json:"translations"`
	HasBackend    bool                         `json:"has_backend"`
	Connect       []string                     `json:"connect"`
}

type packageKindDTO struct {
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	NewLabel    string `json:"new_label"`
	DefaultName string `json:"default_name"`
	NewHint     string `json:"new_hint"`
	IconURL     string `json:"icon_url"`
}

func toPackageDTO(p *exthost.PackageInfo) *packageDTO {
	if p == nil {
		return nil
	}
	out := &packageDTO{
		Digest: p.Digest, EntryURL: p.EntryURL, IconURL: p.IconURL,
		Notifications: p.Notifications, Translations: p.Translations,
		HasBackend: p.HasBackend, Connect: p.Connect, Kinds: []packageKindDTO{},
	}
	if out.Notifications == nil {
		out.Notifications = map[string]string{}
	}
	if out.Translations == nil {
		out.Translations = map[string]map[string]string{}
	}
	if out.Connect == nil {
		out.Connect = []string{}
	}
	for _, k := range p.Kinds {
		out.Kinds = append(out.Kinds, packageKindDTO{
			Kind: k.Kind, Label: k.Label, NewLabel: k.NewLabel, DefaultName: k.DefaultName,
			NewHint: k.NewHint, IconURL: k.IconURL,
		})
	}
	return out
}

// adminAppDTO is an installed package as Admin › Apps manages it.
type adminAppDTO struct {
	ID                 string `json:"id"`
	Version            string `json:"version"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
	BackendURL         string `json:"backend_url"`
	ManifestBackendURL string `json:"manifest_backend_url"`
	HasSecret          bool   `json:"has_secret"`
	InstalledAt        string `json:"installed_at"`
	UpdatedAt          string `json:"updated_at"`
}

func toAdminAppDTO(a *exthost.PackageAdmin) adminAppDTO {
	return adminAppDTO{
		ID: a.ID, Version: a.Version, Digest: a.Digest, Size: a.Size,
		BackendURL: a.BackendURL, ManifestBackendURL: a.ManifestBackendURL, HasSecret: a.HasSecret,
		InstalledAt: a.InstalledAt.UTC().Format(time.RFC3339),
		UpdatedAt:   a.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Server) appsError(w http.ResponseWriter, r *http.Request, err error) {
	var bad *apppkg.Error
	var invalid *exthost.InvalidError
	switch {
	case errors.As(err, &bad):
		writeProblem(w, http.StatusBadRequest, "Bad Request", bad.Msg)
	case errors.As(err, &invalid):
		writeProblem(w, http.StatusBadRequest, "Bad Request", invalid.Msg)
	case errors.Is(err, exthost.ErrInstalled):
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
	case errors.Is(err, exthost.ErrNoSecretKey):
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable", err.Error())
	default:
		s.extError(w, r, err)
	}
}

// readPackageUpload reads the multipart field "package" whole, refusing more
// than apppkg.MaxUploadBytes.
func readPackageUpload(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, apppkg.MaxUploadBytes+64<<10)
	mr, err := r.MultipartReader()
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "send the package as multipart/form-data, field \"package\"")
		return nil, false
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "a package is at most 20 MiB")
				return nil, false
			}
			writeProblem(w, http.StatusBadRequest, "Bad Request", "unreadable upload")
			return nil, false
		}
		if part.FormName() != "package" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, apppkg.MaxUploadBytes+1))
		_ = part.Close()
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) || len(data) > apppkg.MaxUploadBytes {
			writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "a package is at most 20 MiB")
			return nil, false
		}
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "unreadable upload")
			return nil, false
		}
		return data, true
	}
	writeProblem(w, http.StatusBadRequest, "Bad Request", "the field \"package\" is missing")
	return nil, false
}

// handleInstallApp is POST /admin/apps[?upgrade=true][&id=]: install an
// uploaded package, or replace the installed version of the same id; id, when
// given, is the app the upload must be. A new app is off until an
// administrator switches it on, like every app.
func (s *Server) handleInstallApp(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	if s.ext == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", exthost.ErrUnknownExt.Error())
		return
	}
	data, ok := readPackageUpload(w, r)
	if !ok {
		return
	}
	b, err := apppkg.Read(data)
	if err != nil {
		s.appsError(w, r, err)
		return
	}
	upgrade := r.URL.Query().Get("upgrade") == "true"
	// An update picked for one app must not install another by mistake.
	if want := r.URL.Query().Get("id"); want != "" && want != b.Manifest.ID {
		writeProblem(w, http.StatusBadRequest, "Bad Request",
			"this package is the app \""+b.Manifest.ID+"\", not \""+want+"\"")
		return
	}
	previous := ""
	if a, err := s.ext.Package(b.Manifest.ID); err == nil {
		previous = a.Version
	}
	a, err := s.ext.Install(r.Context(), b, admin.ID, upgrade)
	if err != nil {
		s.appsError(w, r, err)
		return
	}
	kind := audit.KindAppInstalled
	detail := map[string]any{"app": a.ID, "version": a.Version, "digest": a.Digest}
	status := http.StatusCreated
	if previous != "" {
		kind = audit.KindAppUpgraded
		detail["previous_version"] = previous
		status = http.StatusOK
	}
	s.audit.Record(r, audit.Event{Kind: kind, UserID: admin.ID, Subject: admin.Email, Detail: detail})
	writeJSON(w, status, toAdminAppDTO(a))
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	if s.ext == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", exthost.ErrUnknownExt.Error())
		return
	}
	a, err := s.ext.Package(chi.URLParam(r, "extID"))
	if err != nil {
		s.appsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminAppDTO(a))
}

// handleUpdateApp is PATCH /admin/apps/{id}: the backend address, overriding
// the manifest's ("" returns to it).
func (s *Server) handleUpdateApp(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	if s.ext == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", exthost.ErrUnknownExt.Error())
		return
	}
	var body struct {
		BackendURL *string `json:"backend_url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil || body.BackendURL == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "backend_url is required")
		return
	}
	id := chi.URLParam(r, "extID")
	a, err := s.ext.SetBackendURL(r.Context(), id, strings.TrimSpace(*body.BackendURL), admin.ID)
	if err != nil {
		s.appsError(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind: audit.KindAppConfigured, UserID: admin.ID, Subject: admin.Email,
		Detail: map[string]any{"app": id, "backend_url": a.BackendURL},
	})
	writeJSON(w, http.StatusOK, toAdminAppDTO(a))
}

// handleRotateAppSecret is POST /admin/apps/{id}/backend-secret: a new key
// for backend tokens, shown this once. The previous one stops working.
func (s *Server) handleRotateAppSecret(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	if s.ext == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", exthost.ErrUnknownExt.Error())
		return
	}
	id := chi.URLParam(r, "extID")
	secret, a, err := s.ext.RotateBackendSecret(r.Context(), id, admin.ID)
	if err != nil {
		s.appsError(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind: audit.KindAppConfigured, UserID: admin.ID, Subject: admin.Email,
		Detail: map[string]any{"app": id, "backend_secret": "rotated"},
	})
	writeJSON(w, http.StatusOK, struct {
		Secret string      `json:"secret"`
		App    adminAppDTO `json:"app"`
	}{secret, toAdminAppDTO(a)})
}

// handleRemoveApp is DELETE /admin/apps/{id}[?purge=true].
func (s *Server) handleRemoveApp(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	if s.ext == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", exthost.ErrUnknownExt.Error())
		return
	}
	id := chi.URLParam(r, "extID")
	purge := r.URL.Query().Get("purge") == "true"
	if err := s.ext.Remove(r.Context(), id, purge); err != nil {
		s.appsError(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind: audit.KindAppRemoved, UserID: admin.ID, Subject: admin.Email,
		Detail: map[string]any{"app": id, "purged": purge},
	})
	w.WriteHeader(http.StatusNoContent)
}

// appAssetSwitch hands a request for a packaged app's files to the asset
// handler before anything else sees it — like artifactSwitch, and for the
// same reason: the app's security headers (X-Frame-Options: DENY) would stop
// the frame. Files are served under exthost.AssetPath on PP_ARTIFACT_URL's
// host when it is set, else on this origin.
func (s *Server) appAssetSwitch() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if s.ext == nil {
			return next
		}
		assets := s.appAssetRouter()
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			if !strings.HasPrefix(p, exthost.AssetPath+"/") {
				next.ServeHTTP(w, r)
				return
			}
			onArtifactHost := s.artifactHost != "" && strings.EqualFold(r.Host, s.artifactHost)
			if s.artifactHost == "" || onArtifactHost {
				assets.ServeHTTP(w, r)
				return
			}
			// Isolated deployment: the app origin carries no app files.
			http.NotFound(w, r)
		})
	}
}

func (s *Server) appAssetRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if len(s.trustedProxies) > 0 {
		r.Use(middleware.ClientIPFromXFF(s.trustedProxies...))
	}
	limit := newLimiter(600, 200)
	r.Use(s.observeArtifact, recoverer, limit.middleware)
	compress := middleware.Compress(5,
		"text/html", "text/css", "text/plain", "text/markdown",
		"text/javascript", "application/javascript", "application/json",
		"image/svg+xml", "application/wasm")
	serve := s.serveAppAsset(compress)
	r.Get(exthost.AssetPath+"/{appID}/{digest}/*", serve)
	r.Head(exthost.AssetPath+"/{appID}/{digest}/*", serve)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		s.appAssetHeaders(w, "")
		http.NotFound(w, r)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	return r
}

// appCSP is the policy a packaged app's files run under: the M50 sandbox
// (an opaque origin, no cookie, no reach into the app), scripts and styles
// from this app's own files only, and network access to nothing but the
// origins its manifest declares. Talking to pp-hub is the bridge's job.
func (s *Server) appCSP(appID string, connect []string) string {
	own := s.ext.AssetBase() + "/" + appID + "/"
	cdn := ""
	if s.sysconfig != nil {
		if list := s.sysconfig.ArtifactCDNAllowlist(); len(list) > 0 {
			cdn = " " + strings.Join(list, " ")
		}
	}
	conn := "'none'"
	if len(connect) > 0 {
		conn = strings.Join(connect, " ")
	}
	return "sandbox allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads; " +
		"default-src 'none'; " +
		"script-src " + own + " 'unsafe-inline' 'unsafe-eval'" + cdn + "; " +
		"style-src " + own + " 'unsafe-inline'" + cdn + "; " +
		"img-src " + own + " data: blob:" + cdn + "; " +
		"font-src " + own + " data:" + cdn + "; " +
		"media-src " + own + " data: blob:; " +
		"connect-src " + conn + "; form-action 'none'; base-uri " + own + "; frame-src 'none'; worker-src " + own + " blob:; " +
		"frame-ancestors " + OriginOf(s.externalURL)
}

func (s *Server) appAssetHeaders(w http.ResponseWriter, csp string) {
	h := w.Header()
	if csp == "" {
		csp = "sandbox; default-src 'none'; frame-ancestors 'none'"
	}
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	// The frame's origin is opaque, so whatever it loads of its own files —
	// module scripts, fonts, fetches — is a cross-origin request.
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
}

// serveAppAsset answers GET/HEAD /x/<id>/<digest>/<path>. The digest pins the
// version, so a response never changes and is cached for good.
func (s *Server) serveAppAsset(compress func(http.Handler) http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "appID")
		connect, ok := s.ext.Connect(id)
		if !ok {
			s.appAssetHeaders(w, "")
			http.NotFound(w, r)
			return
		}
		s.appAssetHeaders(w, s.appCSP(id, connect))
		p := chi.URLParam(r, "*")
		if p == "" || strings.HasSuffix(p, "/") {
			p += "index.html"
		}
		f, err := s.store.GetExtPackageFile(r.Context(), store.GetExtPackageFileParams{
			ExtID: id, Digest: chi.URLParam(r, "digest"), Path: p,
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.ErrorContext(r.Context(), "app asset", "app", id, "err", err)
			}
			http.NotFound(w, r)
			return
		}
		h := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", f.ContentType)
			w.Header().Set("ETag", `"`+f.Sha256+`"`)
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.Body))
		}
		if r.Header.Get("Range") == "" {
			compress(http.HandlerFunc(h)).ServeHTTP(w, r)
			return
		}
		h(w, r)
	}
}
