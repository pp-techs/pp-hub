package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/groups"
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
	store         *store.Queries
	externalURL   string
	onlyOfficeURL string
	wopiServerURL string
}

func NewServer(a *auth.Service, sp *spaces.Service, f *fs.Service, sh *share.Service, wp *wopi.Service, pv *preview.Worker, us *users.Service, q *store.Queries, externalURL, onlyOfficeURL, wopiServerURL string) *Server {
	return &Server{auth: a, spaces: sp, groups: groups.NewService(q), fs: f, share: sh, wopi: wp, preview: pv, users: us, activity: activity.NewService(q), store: q, externalURL: externalURL, onlyOfficeURL: onlyOfficeURL, wopiServerURL: wopiServerURL}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Get("/auth/login", s.auth.HandleLogin)
	r.Get("/auth/callback", s.auth.HandleCallback)
	r.Post("/auth/logout", s.auth.HandleLogout)

	r.Route("/api/v1", func(r chi.Router) {
		// Everything below this line is authenticated.
		//
		// This used to be a call at the top of each handler: seventy-odd
		// handlers, thirty-odd guards, and nothing but eyesight connecting
		// the two numbers — a new route was public until someone noticed.
		// TestEveryAPIRouteIsAuthenticated walks the router and holds the line.
		r.Use(s.authenticated)

		r.Get("/users/me", s.handleGetMyProfile)
		r.Put("/users/me", s.handleUpdateMyProfile)
		r.Post("/users/me/avatar", s.handleUploadAvatar)
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
		r.Post("/invitations/accept", s.handleAcceptInvitation)
		r.Get("/admin/stats", s.handleAdminStats)
		r.Patch("/admin/users/{userID}", s.handleAdminUpdateUser)
		r.Get("/users", s.handleListUsers)
		r.Get("/users/search", s.handleSearchUsers)

		r.Route("/spaces", func(r chi.Router) {
			r.Post("/", s.handleCreateSpace)
			r.Get("/", s.handleListSpaces)
			r.Route("/{spaceID}", func(r chi.Router) {
				r.Patch("/", s.handleUpdateSpace)
				r.Delete("/", s.handleDeleteSpace)
				r.Get("/invitations", s.handleListInvitations)
				r.Post("/invitations", s.handleCreateInvitation)
				r.Delete("/invitations/{inviteID}", s.handleRevokeInvitation)
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
						r.Patch("/", s.handleSetMemberAdmin)
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

	r.Route("/wopi/files/{nodeID}", func(r chi.Router) {
		r.Get("/", s.handleWopiCheckFileInfo)
		r.Get("/contents", s.handleWopiGetFile)
		r.Post("/contents", s.handleWopiPutFile)
		r.Post("/", s.handleWopiLockOps)
	})

	r.Route("/s/{token}", func(r chi.Router) {
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
