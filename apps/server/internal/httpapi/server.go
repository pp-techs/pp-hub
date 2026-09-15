package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
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
	fs            *fs.Service
	share         *share.Service
	wopi          *wopi.Service
	preview       *preview.Worker
	users         *users.Service
	store         *store.Queries
	externalURL   string
	onlyOfficeURL string
	wopiServerURL string
}

func NewServer(a *auth.Service, sp *spaces.Service, f *fs.Service, sh *share.Service, wp *wopi.Service, pv *preview.Worker, us *users.Service, q *store.Queries, externalURL, onlyOfficeURL, wopiServerURL string) *Server {
	return &Server{auth: a, spaces: sp, fs: f, share: sh, wopi: wp, preview: pv, users: us, store: q, externalURL: externalURL, onlyOfficeURL: onlyOfficeURL, wopiServerURL: wopiServerURL}
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
		r.Get("/me", s.handleMe)
		r.Get("/users/me", s.handleGetMyProfile)
		r.Put("/users/me", s.handleUpdateMyProfile)
		r.Post("/users/me/avatar", s.handleUploadAvatar)
		r.Get("/users", s.handleListUsers)       // admin only
		r.Get("/users/search", s.handleSearchUsers) // autocomplete

		r.Route("/spaces", func(r chi.Router) {
			r.Post("/", s.handleCreateSpace)
			r.Get("/", s.handleListSpaces)
			r.Route("/{spaceID}", func(r chi.Router) {
				r.Get("/children", s.handleListChildren)
				r.Get("/tree", s.handleListTree)
				r.Get("/trash", s.handleListDeleted)
				r.Get("/shared", s.handleListShared)
				r.Post("/dirs", s.handleMkdir)
				r.Post("/files", s.handleUpload)
				r.Route("/members", func(r chi.Router) {
					r.Get("/", s.handleListMembers)
					r.Post("/", s.handleAddMember)
					r.Route("/{userID}", func(r chi.Router) {
						r.Patch("/", s.handleUpdateMemberRole)
						r.Delete("/", s.handleRemoveMember)
					})
				})
			})
		})

		r.Route("/nodes/{nodeID}", func(r chi.Router) {
			r.Get("/download", s.handleDownload)
			r.Get("/content", s.handleContent)
			r.Patch("/rename", s.handleRenameNode)
			r.Patch("/move", s.handleMoveNode)
			r.Delete("/", s.handleDeleteNode)
			r.Get("/versions", s.handleListVersions)
			r.Post("/versions/{versionID}/restore", s.handleRestoreVersion)
			r.Post("/shares", s.handleCreateShare)
			r.Get("/shares", s.handleListShares)
			r.Post("/edit-session", s.handleEditSession)
			r.Get("/preview", s.handlePreview)
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
	})
	return r
}

// requireUser returns the authenticated user or writes 401 and returns nil.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) *store.User {
	u := s.auth.CurrentUser(r)
	if u == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "")
		return nil
	}
	return u
}

type meResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	id, _ := u.ID.Value()
	writeJSON(w, http.StatusOK, meResponse{
		ID:    id.(string),
		Email: u.Email,
		Name:  u.Name,
	})
}
