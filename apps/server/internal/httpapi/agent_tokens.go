package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Personal access tokens for agents (M41), on the account's Security pane.
// Mounted only on the network's /api/v1 table, never on the agent router: a
// credential an agent could mint for itself would outlive every switch meant
// to stop it.

// defaultAgentTokenDays is the lifetime when the caller names none.
const defaultAgentTokenDays = 30

type agentTokenDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Purpose    string `json:"purpose"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	ExpiresAt  string `json:"expires_at"`
	// Token is the secret itself, present only in the answer that created it.
	Token string `json:"token,omitempty"`
}

func agentTokenToDTO(t store.AgentToken) agentTokenDTO {
	d := agentTokenDTO{
		ID:        t.ID.String(),
		Name:      t.Name,
		Purpose:   t.Purpose,
		CreatedAt: t.CreatedAt.Time.Format(timeLayout),
		ExpiresAt: t.ExpiresAt.Time.Format(timeLayout),
	}
	if t.LastUsedAt.Valid {
		d.LastUsedAt = t.LastUsedAt.Time.Format(timeLayout)
	}
	return d
}

// mcpTokensAllowed is whether tokens may be minted and presented: agents on
// at all, and tokens not turned off.
func (s *Server) mcpTokensAllowed() bool { return s.mcpEnabled() && s.sysconfig.MCPTokens() }

// tokensAllowed is whether a token of purpose may be minted now: an agent
// token under the MCP switches, a WebDAV token while WebDAV is on.
func (s *Server) tokensAllowed(p auth.TokenPurpose) bool {
	if p == auth.TokenPurposeDAV {
		return s.davEnabled()
	}
	return s.mcpTokensAllowed()
}

// handleListAgentTokens is GET /users/me/agent-tokens: the caller's live
// tokens. Listed even while tokens are off, so they can still be revoked.
func (s *Server) handleListAgentTokens(w http.ResponseWriter, r *http.Request) {
	rows, err := s.auth.ListAgentTokens(r.Context(), caller(r).ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]agentTokenDTO, 0, len(rows))
	for _, t := range rows {
		out = append(out, agentTokenToDTO(t))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateAgentToken is POST /users/me/agent-tokens
// {name, expires_in_days, purpose}; purpose is "mcp" (the default) or "dav".
func (s *Server) handleCreateAgentToken(w http.ResponseWriter, r *http.Request) {
	if !limitBody(w, r, 4<<10) {
		return
	}
	var body struct {
		Name          string `json:"name"`
		ExpiresInDays int    `json:"expires_in_days"`
		Purpose       string `json:"purpose"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	purpose, ok := auth.ParseTokenPurpose(body.Purpose)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "purpose must be mcp or dav")
		return
	}
	if !s.tokensAllowed(purpose) {
		detail := "personal tokens for agents are turned off on this instance"
		if purpose == auth.TokenPurposeDAV {
			detail = "WebDAV access is turned off on this instance"
		}
		writeProblem(w, http.StatusForbidden, "Forbidden", detail)
		return
	}
	if body.ExpiresInDays == 0 {
		body.ExpiresInDays = defaultAgentTokenDays
	}
	tok, raw, err := s.auth.CreateAgentToken(r, *caller(r), body.Name, time.Duration(body.ExpiresInDays)*24*time.Hour, purpose)
	if errors.Is(err, auth.ErrBadAgentToken) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if err != nil {
		Error(w, r, err)
		return
	}
	dto := agentTokenToDTO(tok)
	dto.Token = raw
	writeJSON(w, http.StatusCreated, dto)
}

// handleRevokeAgentToken is DELETE /users/me/agent-tokens/{tokenID}.
func (s *Server) handleRevokeAgentToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuidParam(r, "tokenID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid token id")
		return
	}
	ok, err := s.auth.RevokeAgentToken(r, *caller(r), id)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !ok {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
