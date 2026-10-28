package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/comments"
	"github.com/nhatbui/pp-hub/apps/server/internal/exthost"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/groups"
	"github.com/nhatbui/pp-hub/apps/server/internal/guest"
	"github.com/nhatbui/pp-hub/apps/server/internal/mail"
	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/notify"
	"github.com/nhatbui/pp-hub/apps/server/internal/obs"
	"github.com/nhatbui/pp-hub/apps/server/internal/officeedit"
	"github.com/nhatbui/pp-hub/apps/server/internal/orgsync"
	"github.com/nhatbui/pp-hub/apps/server/internal/personal"
	"github.com/nhatbui/pp-hub/apps/server/internal/preview"
	"github.com/nhatbui/pp-hub/apps/server/internal/publish"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/sysconfig"
	"github.com/nhatbui/pp-hub/apps/server/internal/unfurl"
	"github.com/nhatbui/pp-hub/apps/server/internal/uploads"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
	"github.com/nhatbui/pp-hub/apps/server/internal/wopi"
)

type Server struct {
	auth      *auth.Service
	spaces    *spaces.Service
	groups    *groups.Service
	fs        *fs.Service
	share     *share.Service
	wopi      *wopi.Service
	preview   *preview.Worker
	users     *users.Service
	activity  *activity.Service
	audit     *audit.Service
	sysconfig *sysconfig.Service
	store     *store.Queries
	pool      *pgxpool.Pool
	blobs     blob.Store
	orgs      *orgsync.Service
	uploads   *uploads.Service
	collab    collab.Service
	// officeEditor only calls the isolated Office runner; nil disables edits.
	officeEditor  officeedit.Engine
	webhookKey    string
	externalURL   string
	onlyOfficeURL string
	wopiServerURL string
	// maxUploadBytes caps any request body that becomes file content. 0
	// disables the cap, which only an operator who means it should choose.
	mail           *mail.Sender
	notify         *notify.Notifier
	maxUploadBytes int64
	// trustedProxies are the CIDRs whose X-Forwarded-For is believed.
	trustedProxies []string
	// metrics records the HTTP series; nil disables them.
	metrics *obs.HTTPMetrics
	// comments and notifications are M29's threads and in-app
	// notifications; notifications also owns the live streams.
	comments      *comments.Service
	notifications *notifications.Service
	// unfurl fetches link bookmarks; nil fetches public addresses only
	// (publicUnfurl). Tests give it one that may reach an httptest server.
	unfurl *unfurl.Fetcher
	// agentRouter answers agent tool calls in process (agent.go); mcpLimit
	// is the MCP endpoint's per-person rate limit (mcp.go).
	agentRouter http.Handler
	mcpLimit    *limiter
	// dav is what the WebDAV surface remembers between requests; davLimit
	// is its per-person limit and davAuthFail the per-address limit on
	// refused credentials (dav.go).
	dav         *davState
	davLimit    *limiter
	davAuthFail *limiter
	// personal provisions personal spaces (sign-in does it too; the space
	// list catches sessions that predate the setting). Nil in handler tests.
	personal *personal.Service
	// guest turns a one-time code into a guest session (guest.go). Nil in
	// handler tests that do not exercise it, which leaves guests off.
	guest *guest.Service
	// ext is the extension host (M44); nil when a build carries none, in
	// which case no extension route exists and every extension listing is empty.
	ext *exthost.Registry
	// web serves the single-page app next to the API; nil leaves the server
	// API-only (development, handler tests).
	web *WebApp
	// publish is the publishing model (M50); nil or not Enabled means no
	// publishing. artifactHost is PP_ARTIFACT_URL's host when one is
	// configured; Router hands that host, or else this origin's /a/, to the
	// artifact router and nothing else (artifact.go).
	publish      *publish.Service
	artifactHost string
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
	Uploads *uploads.Service
	// Collab hosts the block editor's rooms.
	Collab collab.Service
	// OfficeEditor processes Office bytes in the isolated runner; nil leaves
	// read/edit unavailable while the operation schema remains readable.
	OfficeEditor officeedit.Engine
	// LogtoWebhookKey signs /hooks/logto deliveries. Empty disables the
	// route: an unauthenticated endpoint that mutates membership is worse
	// than a slower path to the same state.
	LogtoWebhookKey string
	ExternalURL     string
	OnlyOfficeURL   string
	WopiServerURL   string
	MaxUploadBytes  int64
	TrustedProxies  []string
	SysConfig       *sysconfig.Service
	Mail            *mail.Sender
	Notifier        *notify.Notifier
	// Notifications is the process's one notifications service: its broker
	// holds the open streams, and the mention job delivers through it too.
	Notifications *notifications.Service
	// Metrics records the HTTP series; nil disables them.
	Metrics *obs.HTTPMetrics
	// Ext is the extension host; nil serves a build with no extensions.
	Ext *exthost.Registry
	// Web serves the built single-page app on the API's origin; nil serves
	// the API only.
	Web *WebApp
	// Publish is the publishing model; nil disables publishing and the
	// artifact host.
	Publish *publish.Service
}

func NewServer(d Deps) *Server {
	s := &Server{
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
		uploads:        d.Uploads,
		collab:         d.Collab,
		officeEditor:   d.OfficeEditor,
		webhookKey:     d.LogtoWebhookKey,
		externalURL:    d.ExternalURL,
		onlyOfficeURL:  d.OnlyOfficeURL,
		wopiServerURL:  d.WopiServerURL,
		maxUploadBytes: d.MaxUploadBytes,
		trustedProxies: d.TrustedProxies,
		sysconfig:      d.SysConfig,
		mail:           d.Mail,
		notify:         d.Notifier,
		comments:       comments.NewService(d.Store, d.Notifications),
		notifications:  d.Notifications,
		metrics:        d.Metrics,
		ext:            d.Ext,
		web:            d.Web,
		publish:        d.Publish,
	}
	if d.Ext != nil {
		s.useExt(d.Ext)
	}
	if d.Publish.Isolated() {
		u, err := url.Parse(d.Publish.ArtifactURL())
		if err != nil {
			panic("publish: artifact URL was validated at startup: " + err.Error())
		}
		s.artifactHost = strings.ToLower(u.Host)
	}
	if s.sysconfig == nil {
		s.sysconfig = sysconfig.NewService(d.Store, nil)
	}
	s.personal = personal.New(d.Store, d.Spaces, d.Share, s.sysconfig, s.audit)
	s.guest = guest.New(d.Store, s.sysconfig, d.Notifier)
	return s
}

// useExt mounts an extension host on this server and gives it the session
// facts only the HTTP edge knows. Routes are added when Router runs.
func (s *Server) useExt(h *exthost.Registry) {
	s.ext = h
	h.Bind(
		func(r *http.Request) store.User { return *caller(r) },
		func(r *http.Request) bool { return s.streamSessionActive(r, callerSession(r)) },
	)
}

func (s *Server) uploadCap() int64 {
	if s.sysconfig != nil {
		return s.sysconfig.MaxUploadBytes("default")
	}
	return s.maxUploadBytes
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	// Published sites first, before anything else is attached: a request
	// for PP_ARTIFACT_URL's host — or, with none, for /a/ — gets the
	// artifact router and never reaches the session, the security headers,
	// the API or the app. Packaged apps' files (/x/, M51) the same way.
	r.Use(s.appAssetSwitch(), s.artifactSwitch(s.artifactRouter()))
	r.Use(middleware.RequestID)
	// chi's RealIP is deprecated in this version precisely because it
	// believes True-Client-IP / X-Real-IP / X-Forwarded-For from anyone, and
	// the whole value of the per-IP limits below is that the caller cannot
	// pick their own key. ClientIPFromXFF only accepts a forwarded address
	// vouched for by a configured proxy, and sets none at all otherwise —
	// which audit.ClientIP reads as "believe the socket". It runs before
	// observe so the access log records the same address.
	if len(s.trustedProxies) > 0 {
		r.Use(middleware.ClientIPFromXFF(s.trustedProxies...))
	}
	r.Use(s.observe, recoverer)
	if s.web != nil {
		r.Use(s.web.Secure, s.web.shareLanding)
		// Whatever no route below claims is the app: a file from the bundle,
		// or index.html for a client-side route. The handler itself refuses
		// paths the API owns, so a mistyped API call is a 404, not HTML.
		r.NotFound(s.web.Handler().ServeHTTP)
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
	// Guest codes are mail to an address somebody else chose, so the
	// per-IP bucket is small; the per-address allowance in internal/guest
	// is the real bound.
	guestCodeLimit := newLimiter(1, 10)
	guestVerifyLimit := newLimiter(10, 20)
	// A publication token is a link token; its password is the guessable
	// part, so unlock gets a far smaller bucket.
	viewerLimit := newLimiter(60, 20)
	unlockLimit := newLimiter(10, 5)

	r.With(loginLimit.middleware).Get("/auth/login", s.auth.HandleLogin)
	r.With(loginLimit.middleware).Get("/auth/callback", s.auth.HandleCallback)
	r.With(loginLimit.middleware, s.sameOrigin).Post("/auth/logout", s.auth.HandleLogout)
	// A guest proves an address with a one-time code (guest.go). Outside
	// /api/v1 because the caller has no session yet; same-origin like the
	// other cookie-setting POST.
	r.With(guestCodeLimit.middleware, s.sameOrigin).Post("/auth/guest/code", s.handleGuestCode)
	r.With(guestVerifyLimit.middleware, s.sameOrigin).Post("/auth/guest/verify", s.handleGuestVerify)
	r.With(loginLimit.middleware).Get("/auth/guest", s.handleGuestConfig)

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
		s.apiRoutes(r, false)
	})

	// Outside /api/v1 deliberately: that group is authenticated and
	// same-origin by construction, and this caller is neither. Its
	// credential is the HMAC over the body. See handleLogtoWebhook.
	r.With(apiLimit.middleware).Post("/hooks/logto", s.handleLogtoWebhook)

	// Agents over MCP (M41). Outside /api/v1 for the reason /wopi/ is: the
	// credential is a bearer token, never the session cookie, so neither
	// the same-origin check nor the cookie lookup applies. mcpBearer
	// authenticates it on every request. See mcp.go.
	s.mcpRoutes(r)

	// WebDAV (dav.go). Outside /api/v1 for the same reason: a token in the
	// Authorization header on every request, never the cookie. It answers
	// through the agent router mcpRoutes builds.
	s.davRoutes(r)

	// Packaged apps' backends calling back (M51). Outside /api/v1 for the
	// same reason again: a bearer token signed with the app's secret, never
	// a cookie. See exthost/backend.go.
	if s.ext != nil {
		r.Group(func(r chi.Router) {
			r.Use(apiLimit.middleware)
			s.ext.MountBackendAPI(r)
		})
	}

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
		// A block document through the link: its tree for the read-only
		// view, and its attachments. Reading, so view is enough and the
		// link's download flag is not consulted.
		r.Get("/document", s.handleShareDocument)
		r.Get("/attachments/{hash}", s.handleShareAttachment)
		// A document's relative references (`![](images/a.png)`): the
		// file's bytes on the document's terms, and what a link names.
		r.Get("/relative", s.handleShareRelative)
		r.Get("/relative/meta", s.handleShareRelativeMeta)
		// Pages (M43) through the link: read-only, ancestors bounded by
		// the link's root.
		r.Get("/page", s.handleSharePage)
		r.Get("/pages", s.handleSharePages)
		r.Get("/edit-session", s.handleShareEditSession)
	})

	// Publishing's viewer API (M50). Anonymous like /s; the browser
	// navigation to the same URL is the SPA (shareLanding). unlock is a
	// POST from the SPA on this origin, so the same-origin check applies.
	r.Route("/p/{token}", func(r chi.Router) {
		r.Use(viewerLimit.middleware)
		r.Get("/", s.handlePublicationInfo)
		r.With(unlockLimit.middleware, s.sameOrigin).Post("/unlock", s.handlePublicationUnlock)
	})
	return r
}

// WopiRouter is the WOPI surface: the routes the Document Server (and the
// worker's office conversion) call back into. It is a router of its own,
// served on its own listener (Config.WopiAddr), and is not reachable through
// Router.
//
// That is the point. These routes are authorized by a signed access token
// carried in the URL, and they are exempt from the same-origin check (the
// Document Server sends no Origin) and from the rate limiter. Behind the
// public listener they would turn a credential that only matters inside the
// compose network into an internet-wide bearer token. Keeping them on a port
// nothing publishes makes that a property of the topology instead of a rule
// every reverse proxy has to remember.
func (s *Server) WopiRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, s.observe, recoverer)
	r.Route("/wopi/files/{nodeID}", func(r chi.Router) {
		r.Get("/", s.handleWopiCheckFileInfo)
		r.Get("/contents", s.handleWopiGetFile)
		r.Post("/contents", s.handleWopiPutFile)
		r.Post("/", s.handleWopiLockOps)
	})
	return r
}

// apiRoutes is the /api/v1 table. The network mounts it behind the rate
// limit, the same-origin check and the session cookie; the agent dispatcher
// (agent.go) mounts the same table in process, so a tool reaches exactly the
// handlers — authorization, audit, notifications — the web client does.
//
// agent adds the routes only that dispatcher mounts: a document's text, read
// and written whole (agent_text.go). The web client edits documents through
// the collaboration room and never needs them, so the network does not carry
// a second way to write file content. The reverse holds for credentials:
// sessions and personal tokens are managed by the person, never by an agent
// acting for them, so the dispatcher does not mount those routes at all.
func (s *Server) apiRoutes(r chi.Router, agent bool) {
	r.Get("/users/me", s.handleGetMyProfile)
	r.Put("/users/me", s.handleUpdateMyProfile)
	r.Post("/users/me/avatar", s.handleUploadAvatar)
	r.Patch("/users/me/preferences", s.handleUpdateMyPreferences)
	if !agent {
		r.Route("/users/me/sessions", func(r chi.Router) {
			r.Get("/", s.handleListSessions)
			r.Delete("/", s.handleRevokeOtherSessions)
			r.Delete("/{sessionID}", s.handleRevokeSession)
		})
		r.Route("/users/me/agent-tokens", func(r chi.Router) {
			r.Get("/", s.handleListAgentTokens)
			r.Post("/", s.handleCreateAgentToken)
			r.Delete("/{tokenID}", s.handleRevokeAgentToken)
		})
	}
	r.Get("/search", s.handleSearch)
	if agent {
		r.Get("/office/schema", s.handleOfficeSchema)
	}
	r.Get("/activity", s.handleListActivity)
	r.Get("/recent", s.handleListRecent)
	r.Get("/starred", s.handleListStarred)
	r.Get("/usage", s.handleUsage)
	r.Get("/shared", s.handleListShared)
	r.Get("/admin/stats", s.handleAdminStats)
	r.Get("/admin/security-events", s.handleAdminSecurityEvents)
	r.Get("/admin/security-events/export", s.handleAdminSecurityEventsExport)
	r.Patch("/admin/users/{userID}", s.handleAdminUpdateUser)
	r.Get("/admin/settings", s.handleGetAdminSettings)
	r.Patch("/admin/settings", s.handleUpdateAdminSettings)
	r.Delete("/admin/settings/{key}", s.handleDeleteAdminSetting)
	r.Post("/admin/settings/smtp/test", s.handleTestSMTP)
	r.Get("/extensions", s.handleListExtensions)
	r.Patch("/admin/extensions/{extID}", s.handleSetExtensionInstance)
	// Packaged apps (M51): install, upgrade, configure, remove. Putting code
	// into the server, or a key into an app's backend, is a person's act;
	// an agent acting for an administrator gets none of it.
	if !agent {
		r.Post("/admin/apps", s.handleInstallApp)
		r.Get("/admin/apps/{extID}", s.handleGetApp)
		r.Patch("/admin/apps/{extID}", s.handleUpdateApp)
		r.Delete("/admin/apps/{extID}", s.handleRemoveApp)
		r.Post("/admin/apps/{extID}/backend-secret", s.handleRotateAppSecret)
	}
	// Extension routes (M44), under /ext/{id}. Mounted inside this group, so
	// the same limits, origin and session checks cover them; a stream has no
	// place in the agent dispatcher's request/response calls.
	if !agent && s.ext != nil {
		s.ext.Mount(r)
	}
	r.Get("/admin/personal-spaces", s.handleAdminListArchivedPersonal)
	r.Delete("/admin/personal-spaces/{spaceID}", s.handleAdminPurgePersonal)
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
			r.Get("/link-preview", s.handleLinkPreview)
			r.Post("/link", s.handleLinkSpace)
			r.Get("/avatar", s.handleGetSpaceAvatar)
			r.Post("/avatar", s.handleUploadSpaceAvatar)
			r.Delete("/avatar", s.handleClearSpaceAvatar)
			r.Get("/children", s.handleListChildren)
			r.Get("/tree", s.handleListTree)
			r.Get("/trash", s.handleListDeleted)
			r.Post("/trash/empty", s.handleEmptyTrash)
			r.Post("/dirs", s.handleMkdir)
			r.Post("/team-folders", s.handleCreateTeamFolder)
			r.Post("/documents", s.handleCreateDocument)
			r.Post("/ext-nodes", s.handleCreateExtNode)
			r.Put("/extensions/{extID}", s.handleSetExtensionSpace)
			r.Get("/extensions/{extID}/nodes", s.handleListExtNodes)
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
		r.Get("/children", s.handleListNodeChildren)
		r.Get("/path", s.handleNodePath)
		r.Get("/people", s.handleNodePeople)
		r.Get("/download", s.handleDownload)
		r.Get("/content", s.handleContent)
		r.Patch("/rename", s.handleRenameNode)
		r.Patch("/move", s.handleMoveNode)
		r.Get("/move-impact", s.handleMoveImpact)
		r.Post("/copy", s.handleCopyNode)
		r.Delete("/", s.handleDeleteNode)
		r.Get("/versions", s.handleListVersions)
		r.Post("/versions/{versionID}/restore", s.handleRestoreVersion)
		r.Post("/star", s.handleStarNode)
		r.Delete("/star", s.handleUnstarNode)
		r.Post("/restore", s.handleRestoreNode)
		r.Delete("/purge", s.handlePurgeNode)
		r.Post("/shares", s.handleCreateShare)
		r.Get("/shares", s.handleListShares)
		r.Delete("/shares/{shareID}", s.handleRevokeShare)
		r.Get("/pending-shares", s.handleListPendingShares)
		r.Delete("/pending-shares/{pendingID}", s.handleRevokePendingShare)
		r.Get("/inherited-access", s.handleInheritedAccess)
		// Publishing (M50): the node's one publication.
		r.Get("/publication", s.handleGetPublication)
		r.Post("/publication", s.handlePublish)
		r.Patch("/publication", s.handleUpdatePublication)
		r.Delete("/publication", s.handleUnpublish)
		r.Post("/edit-session", s.handleEditSession)
		r.Get("/collab", s.handleCollabSession)
		r.Get("/attachments/{hash}", s.handleAttachment)
		r.Get("/relative", s.handleRelative)
		r.Get("/relative/meta", s.handleRelativeMeta)
		r.Post("/relative/embed", s.handleRelativeEmbed)
		r.Get("/document", s.handleDocument)
		r.Get("/export", s.handleExport)
		// Pages (M43): the page tree, breadcrumb and backlinks.
		r.Get("/page", s.handleNodePage)
		r.Get("/pages", s.handleNodePages)
		r.Get("/backlinks", s.handleNodeBacklinks)
		r.Post("/unfurl", s.handleUnfurl)
		r.Get("/comments", s.handleListComments)
		r.Post("/comment-threads", s.handleCreateCommentThread)
		r.Get("/preview", s.handlePreview)
		r.Get("/grants", s.handleListGrants)
		r.Put("/grants", s.handleUpsertGrant)
		r.Delete("/grants/{groupID}", s.handleRevokeGrant)
		if agent {
			r.Get("/text", s.handleReadText)
			r.Get("/file", s.handleReadFile)
			r.Put("/text", s.handleWriteMarkdown)
			r.Get("/office", s.handleReadOffice)
			r.Put("/office", s.handleEditOffice)
			// publish_artifact's files, written into the folder it
			// publishes (agent_publish.go).
			r.Put("/site-files", s.handleWriteSiteFiles)
		}
	})
	// The block editor's sockets. A GET, so sameOrigin lets it by, and a
	// socket is not a read: socketOrigin demands this deployment's
	// Origin before ygo sees the request (see socketOrigin).
	// TestCollabSocketRefusesForeignOrigins holds the route to it.
	r.With(s.socketOrigin).Get("/collab/{room}", s.handleCollabSocket)
	// The templates a new block document can start from.
	r.Get("/templates/markdown", s.handleListMarkdownTemplates)
	// Comments (M29): a thread's and a comment's routes resolve the
	// document behind them and take download on it, like the node's.
	r.Post("/comment-threads/{threadID}/comments", s.handleReplyComment)
	r.Post("/comment-threads/{threadID}/resolve", s.handleResolveThread)
	r.Post("/comment-threads/{threadID}/reopen", s.handleReopenThread)
	r.Patch("/comments/{commentID}", s.handleEditComment)
	r.Delete("/comments/{commentID}", s.handleDeleteComment)
	// In-app notifications. The stream is Server-Sent Events, which
	// nginx must not buffer (its own location in nginx.conf).
	r.Get("/notifications", s.handleListNotifications)
	r.Get("/notifications/unread-count", s.handleUnreadNotifications)
	r.Post("/notifications/read", s.handleMarkNotificationsRead)
	r.Get("/notifications/stream", s.handleNotificationStream)
	// Resumable uploads (tus 1.0.0). The only way file content enters
	// through the API: a chunk at a time, each one short enough to finish
	// inside whatever request deadline a proxy in front imposes.
	r.Route("/uploads", func(r chi.Router) {
		r.Post("/", s.handleCreateUpload)
		r.Route("/{uploadID}", func(r chi.Router) {
			r.Head("/", s.handleUploadOffset)
			r.Patch("/", s.handleUploadChunk)
			r.Delete("/", s.handleTerminateUpload)
		})
	})
}

// ctxKey is this package's private context-key type; a named type keeps it
// from colliding with a key any other package stores.
type ctxKey int

const (
	sessionCtxKey ctxKey = iota
	logCtxKey
	// mcpSessionCtxKey carries the person an MCP request acts for, from
	// mcpBearer to the server built for the request (mcp.go).
	mcpSessionCtxKey
)

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
		// A guest session reaches a short list of read-and-comment routes
		// and nothing else (guest_routes.go). It is a list of what is
		// allowed, not of what is refused, so a route added later is closed
		// to guests until somebody opens it on purpose.
		if sess.User.Kind == "guest" {
			if !s.guest.Enabled() {
				writeProblem(w, http.StatusForbidden, "Forbidden", "guest access is turned off")
				return
			}
			if !guestMayCall(r) {
				writeProblem(w, http.StatusForbidden, "Forbidden", "a guest cannot do this")
				return
			}
		}
		if rl, ok := r.Context().Value(logCtxKey).(*requestLog); ok {
			rl.userID = sess.User.ID.String()
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
