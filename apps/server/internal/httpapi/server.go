package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/groups"
	"github.com/nhatbui/pp-hub/apps/server/internal/orgsync"
	"github.com/nhatbui/pp-hub/apps/server/internal/preview"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
	"github.com/nhatbui/pp-hub/apps/server/internal/wopi"
)

type Server struct {
	auth          *auth.Service
	spaces        *spaces.Service
	groups        *groups.Service
	fs            *fs.Service
	share         *share.Service
	wopi          *wopi.Service
	preview       *preview.Worker
	users         *users.Service
	activity      *activity.Service
	audit         *audit.Service
	store         *store.Queries
	pool          *pgxpool.Pool
	blobs         blob.Store
	orgs          *orgsync.Service
	webhookKey    string
	externalURL   string
	onlyOfficeURL string
	wopiServerURL string
	// maxUploadBytes caps any request body that becomes file content. 0
	// disables the cap, which only an operator who means it should choose.
	maxUploadBytes int64
	// trustedProxies are the CIDRs whose X-Forwarded-For is believed.
	trustedProxies []string
}

// Deps is what the HTTP edge needs. A struct rather than a parameter list
// because the list had reached eleven positional arguments of which three
// were strings, and the next one added would have been a silent swap waiting
// to happen.
type Deps struct {
	Auth    *auth.Service
	Spaces  *spaces.Service
	FS      *fs.Service
	Share   *share.Service
	Wopi    *wopi.Service
	Preview *preview.Worker
	Users   *users.Service
	Store   *store.Queries
	Pool    *pgxpool.Pool
	Blobs   blob.Store
	OrgSync *orgsync.Service
	// LogtoWebhookKey signs /hooks/logto deliveries. Empty disables the
	// route: an unauthenticated endpoint that mutates membership is worse
	// than a slower path to the same state.
	LogtoWebhookKey string
	ExternalURL     string
	OnlyOfficeURL   string
	WopiServerURL   string
	MaxUploadBytes  int64
	TrustedProxies  []string
}

func NewServer(d Deps) *Server {
	return &Server{
		auth:           d.Auth,
		spaces:         d.Spaces,
		groups:         groups.NewService(d.Store),
		fs:             d.FS,
		share:          d.Share,
		wopi:           d.Wopi,
		preview:        d.Preview,
		users:          d.Users,
		activity:       activity.NewService(d.Store),
		audit:          audit.New(d.Store),
		store:          d.Store,
		pool:           d.Pool,
		blobs:          d.Blobs,
		orgs:           d.OrgSync,
		webhookKey:     d.LogtoWebhookKey,
		externalURL:    d.ExternalURL,
		onlyOfficeURL:  d.OnlyOfficeURL,
		wopiServerURL:  d.WopiServerURL,
		maxUploadBytes: d.MaxUploadBytes,
		trustedProxies: d.TrustedProxies,
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, redactingLogger, middleware.Recoverer)
	// chi's RealIP is deprecated in this version precisely because it
	// believes True-Client-IP / X-Real-IP / X-Forwarded-For from anyone, and
	// the whole value of the per-IP limits below is that the caller cannot
	// pick their own key. ClientIPFromXFF only accepts a forwarded address
	// vouched for by a configured proxy, and sets none at all otherwise —
	// which audit.ClientIP reads as "believe the socket".
	if len(s.trustedProxies) > 0 {
		r.Use(middleware.ClientIPFromXFF(s.trustedProxies...))
	}

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", s.handleReadyz)

	// Rate limits, on the three surfaces an anonymous caller can reach. The
	// sign-in endpoints cost an IdP round trip each; a share token is a
	// secret that can be guessed; and the API is the rest of the server.
	// Buckets are per IP and per limiter, so exhausting one does not spend
	// another.
	loginLimit := newLimiter(30, 10)
	shareLimit := newLimiter(60, 20)
	apiLimit := newLimiter(1200, 200)

	r.With(loginLimit.middleware).Get("/auth/login", s.auth.HandleLogin)
	r.With(loginLimit.middleware).Get("/auth/callback", s.auth.HandleCallback)
	r.With(loginLimit.middleware, s.sameOrigin).Post("/auth/logout", s.auth.HandleLogout)

	r.Route("/api/v1", func(r chi.Router) {
		// Everything below this line is authenticated.
		//
		// This used to be a call at the top of each handler: seventy-odd
		// handlers, thirty-odd guards, and nothing but eyesight connecting
		// the two numbers — a new route was public until someone noticed.
		// TestEveryAPIRouteIsAuthenticated walks the router and holds the line.
		//
		// sameOrigin sits beside it for the same reason: one middleware, not
		// seventy chances to forget. TestEveryAPIMutationChecksOrigin holds
		// that line.
		//
		// sameOrigin runs *before* authenticated: a forged cross-site write
		// should be refused without a session lookup, and the caller should
		// be told the actual reason rather than a 401 that sends them
		// looking for an expired login.
		r.Use(apiLimit.middleware, s.sameOrigin, s.authenticated)

		r.Get("/users/me", s.handleGetMyProfile)
		r.Put("/users/me", s.handleUpdateMyProfile)
		r.Post("/users/me/avatar", s.handleUploadAvatar)
		r.Patch("/users/me/preferences", s.handleUpdateMyPreferences)
		r.Route("/users/me/sessions", func(r chi.Router) {
			r.Get("/", s.handleListSessions)
			r.Delete("/", s.handleRevokeOtherSessions)
			r.Delete("/{sessionID}", s.handleRevokeSession)
		})
		r.Get("/search", s.handleSearch)
		r.Get("/activity", s.handleListActivity)
		r.Get("/recent", s.handleListRecent)
		r.Get("/starred", s.handleListStarred)
		r.Get("/usage", s.handleUsage)
		r.Get("/shared", s.handleListShared)
		r.Get("/admin/stats", s.handleAdminStats)
		r.Get("/admin/security-events", s.handleAdminSecurityEvents)
		r.Patch("/admin/users/{userID}", s.handleAdminUpdateUser)
		r.Get("/users", s.handleListUsers)
		r.Get("/users/{userID}/avatar", s.handleGetAvatar)

		r.Route("/spaces", func(r chi.Router) {
			r.Post("/", s.handleCreateSpace)
			r.Get("/", s.handleListSpaces)
			r.Route("/{spaceID}", func(r chi.Router) {
				r.Patch("/", s.handleUpdateSpace)
				r.Delete("/", s.handleDeleteSpace)
				r.Get("/departures", s.handleListDepartures)
				r.Post("/departures/{userID}/complete", s.handleCompleteDeparture)
				r.Get("/children", s.handleListChildren)
				r.Get("/tree", s.handleListTree)
				r.Get("/trash", s.handleListDeleted)
				r.Post("/trash/empty", s.handleEmptyTrash)
				r.Post("/dirs", s.handleMkdir)
				r.Post("/team-folders", s.handleCreateTeamFolder)
				r.Post("/files", s.handleUpload)
				r.Route("/groups", func(r chi.Router) {
					r.Get("/", s.handleListGroups)
					r.Post("/", s.handleCreateGroup)
					r.Route("/{groupID}", func(r chi.Router) {
						r.Patch("/", s.handleUpdateGroup)
						r.Delete("/", s.handleDeleteGroup)
						r.Get("/members", s.handleListGroupMembers)
						r.Post("/members", s.handleAddGroupMember)
						r.Delete("/members/{userID}", s.handleRemoveGroupMember)
					})
				})
				r.Route("/members", func(r chi.Router) {
					r.Get("/", s.handleListMembers)
					r.Post("/", s.handleAddMember)
					r.Route("/{userID}", func(r chi.Router) {
						r.Post("/set-admin", s.handleSetMemberAdmin)
						r.Delete("/", s.handleRemoveMember)
					})
				})
			})
		})
		r.Route("/nodes/{nodeID}", func(r chi.Router) {
			r.Get("/", s.handleGetNode)
			r.Get("/download", s.handleDownload)
			r.Get("/content", s.handleContent)
			r.Patch("/rename", s.handleRenameNode)
			r.Patch("/move", s.handleMoveNode)
			r.Get("/move-impact", s.handleMoveImpact)
			r.Post("/copy", s.handleCopyNode)
			r.Delete("/", s.handleDeleteNode)
			r.Get("/versions", s.handleListVersions)
			r.Post("/versions", s.handleUploadVersion)
			r.Post("/versions/{versionID}/restore", s.handleRestoreVersion)
			r.Post("/star", s.handleStarNode)
			r.Delete("/star", s.handleUnstarNode)
			r.Post("/restore", s.handleRestoreNode)
			r.Delete("/purge", s.handlePurgeNode)
			r.Post("/shares", s.handleCreateShare)
			r.Get("/shares", s.handleListShares)
			r.Delete("/shares/{shareID}", s.handleRevokeShare)
			r.Post("/edit-session", s.handleEditSession)
			r.Get("/preview", s.handlePreview)
			r.Get("/grants", s.handleListGrants)
			r.Put("/grants", s.handleUpsertGrant)
			r.Delete("/grants/{groupID}", s.handleRevokeGrant)
		})
	})

	// Outside /api/v1 deliberately: that group is authenticated and
	// same-origin by construction, and this caller is neither. Its
	// credential is the HMAC over the body. See handleLogtoWebhook.
	r.With(apiLimit.middleware).Post("/hooks/logto", s.handleLogtoWebhook)

	r.Route("/wopi/files/{nodeID}", func(r chi.Router) {
		r.Get("/", s.handleWopiCheckFileInfo)
		r.Get("/contents", s.handleWopiGetFile)
		r.Post("/contents", s.handleWopiPutFile)
		r.Post("/", s.handleWopiLockOps)
	})

	r.Route("/s/{token}", func(r chi.Router) {
		// A share token is a bearer secret in a URL. Without a limit, it is
		// also an oracle: unlimited guesses against a namespace, from
		// anywhere, with a 200 telling the caller when they hit.
		r.Use(shareLimit.middleware)
		r.Get("/", s.handleShareInfo)
		r.Get("/download", s.handleShareDownload)
		r.Get("/children", s.handleShareChildren)
		r.Get("/content", s.handleShareNodeContent)
		r.Get("/node-download", s.handleShareNodeDownload)
		r.Get("/download-all", s.handleShareDownloadAll)
	})
	return r
}

// ctxKey is this package's private context-key type; a named type keeps it
// from colliding with a key any other package stores.
type ctxKey int

const sessionCtxKey ctxKey = iota

// authenticated resolves the session cookie once, for every route it wraps,
// and rejects the request before any handler runs.
func (s *Server) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := s.auth.Authenticate(w, r)
		if sess == nil {
			writeProblem(w, http.StatusUnauthorized, "Unauthorized", "")
			return
		}
		// Suspension is checked per request rather than at login, so
		// suspending an account ends its live sessions immediately.
		if sess.User.SuspendedAt.Valid {
			writeProblem(w, http.StatusForbidden, "Forbidden", "this account is suspended")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionCtxKey, sess)))
	})
}

// caller is the authenticated user. Never nil inside the authenticated group,
// which is the only place a handler may call it.
func caller(r *http.Request) *store.User { return &callerSession(r).User }

func callerSession(r *http.Request) *auth.Session {
	sess, ok := r.Context().Value(sessionCtxKey).(*auth.Session)
	if !ok {
		// A handler registered outside the authenticated group. That is a
		// wiring bug, not a request the caller can fix, and it fails loudly
		// here rather than silently serving someone else's data.
		panic("httpapi: handler runs outside the authenticated group")
	}
	return sess
}
