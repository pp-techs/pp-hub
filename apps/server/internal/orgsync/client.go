package orgsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

// ErrUnreachable is a transport failure talking to Logto, as opposed to Logto
// answering with a refusal. The two deserve different status codes: one is an
// outage, the other is a rejected request.
var ErrUnreachable = errors.New("orgsync: identity provider unreachable")

// APIError is a non-2xx from the Management API, carrying the status so a
// handler can map 404 and 422 onto something the operator can act on rather
// than a flat 502.
type APIError struct {
	Status int
	Method string
	Path   string
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("logto %s %s: %d %s", e.Method, e.Path, e.Status, e.Body)
}

// Status returns the upstream status of err, or 0 when it is not an APIError.
func Status(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

// Member is an organization member as pp-hub cares about them: the identity
// key, enough profile to pre-create a local row and keep its IdP-owned fields
// current, and whether they hold the admin organization role.
type Member struct {
	ID      string
	Email   string
	Name    string
	Profile users.IdPProfile
	IsAdmin bool
	// Suspended is Logto's isSuspended, nil when the response did not carry
	// it. A pointer because an absent field must never read as "reactivate".
	Suspended *bool
}

// Client talks to the Logto Management API with a machine-to-machine token.
//
// The token is cached and refreshed ahead of expiry. It is not refreshed
// reactively on a 401: a 401 here means the M2M application lost its
// permission, and retrying with a fresh token of the same audience would
// produce the same answer while hiding the cause.
type Client struct {
	cfg  config.LogtoSettings
	http *http.Client

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
	// adminRoleID is resolved once from the organization template. Only the
	// invitation API insists on role *ids*; role assignment takes names.
	adminRoleID string
}

func NewClient(cfg config.LogtoSettings) *Client {
	return &Client{
		cfg: cfg,
		// Every call sits inside a request an admin is waiting on, or inside
		// a job that will be retried. Neither wants an unbounded hang.
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

// accessToken returns a live M2M token, fetching one when the cached copy is
// within a minute of expiry.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.tokenExpiry) > time.Minute {
		return c.token, nil
	}
	form := url.Values{
		"grant_type": {"client_credentials"},
		"resource":   {c.cfg.APIResource},
		"scope":      {"all"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.Endpoint+"/oidc/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: token endpoint: %w", ErrUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Status: resp.StatusCode, Method: "POST", Path: "/oidc/token", Body: string(body)}
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("logto token: %w", err)
	}
	if out.AccessToken == "" {
		return "", errors.New("logto token: response carried no access_token")
	}
	c.token = out.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return c.token, nil
}

// do performs one Management API call. out may be nil for 204 responses.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Endpoint+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s %s: %w", ErrUnreachable, method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{Status: resp.StatusCode, Method: method, Path: path, Body: string(raw)}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// logtoUser is the subset of Logto's user entity this package reads.
//
// avatar, primaryPhone and profile.gender are the same values the ID token
// carries as picture, phone_number and gender, so a reconcile and a sign-in
// agree and neither undoes the other.
type logtoUser struct {
	ID           string `json:"id"`
	PrimaryEmail string `json:"primaryEmail"`
	PrimaryPhone string `json:"primaryPhone"`
	Name         string `json:"name"`
	Username     string `json:"username"`
	Avatar       string `json:"avatar"`
	IsSuspended  *bool  `json:"isSuspended"`
	Profile      struct {
		Gender string `json:"gender"`
	} `json:"profile"`
	OrganizationRoles []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"organizationRoles"`
}

func (u logtoUser) member(adminRole string) Member {
	m := Member{ID: u.ID, Email: u.PrimaryEmail, Name: u.Name, Suspended: u.IsSuspended, Profile: users.IdPProfile{
		Picture: u.Avatar,
		Phone:   u.PrimaryPhone,
		Gender:  u.Profile.Gender,
	}}
	if m.Name == "" {
		m.Name = u.Username
	}
	for _, role := range u.OrganizationRoles {
		if strings.EqualFold(role.Name, adminRole) {
			m.IsAdmin = true
		}
	}
	return m
}

// pageSize is Logto's maximum sensible page. Organizations are companies, not
// mailing lists, so this is one request for almost every real tenant.
const pageSize = 100

// OrgMembers returns every member of an organization, with their admin flag
// resolved against the configured role name.
func (c *Client) OrgMembers(ctx context.Context, orgID string) ([]Member, error) {
	var out []Member
	for page := 1; ; page++ {
		var batch []logtoUser
		path := fmt.Sprintf("/api/organizations/%s/users?page=%d&page_size=%d",
			url.PathEscape(orgID), page, pageSize)
		if err := c.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, u := range batch {
			out = append(out, u.member(c.cfg.AdminRole))
		}
		if len(batch) < pageSize {
			return out, nil
		}
	}
}

// Organization is the upstream's own record of a company: its id and the
// name the space mirrors.
type Organization struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (c *Client) Organization(ctx context.Context, orgID string) (Organization, error) {
	var org Organization
	err := c.do(ctx, http.MethodGet, "/api/organizations/"+url.PathEscape(orgID), nil, &org)
	return org, err
}

func (c *Client) CreateOrganization(ctx context.Context, name string) (Organization, error) {
	var org Organization
	err := c.do(ctx, http.MethodPost, "/api/organizations", map[string]any{"name": name}, &org)
	return org, err
}

func (c *Client) RenameOrganization(ctx context.Context, orgID, name string) error {
	return c.do(ctx, http.MethodPatch, "/api/organizations/"+url.PathEscape(orgID),
		map[string]any{"name": name}, nil)
}

// FindUserByEmail resolves an address to an existing Logto account.
//
// Logto's `q` is a fuzzy match across several fields, so the exact comparison
// happens here: inviting "a@x.com" must never add "aa@x.com" because the
// search ranked it first.
func (c *Client) FindUserByEmail(ctx context.Context, email string) (Member, bool, error) {
	var users []logtoUser
	path := "/api/users?page=1&page_size=20&q=" + url.QueryEscape(email)
	if err := c.do(ctx, http.MethodGet, path, nil, &users); err != nil {
		return Member{}, false, err
	}
	for _, u := range users {
		if strings.EqualFold(u.PrimaryEmail, email) {
			return u.member(c.cfg.AdminRole), true, nil
		}
	}
	return Member{}, false, nil
}

// User reads one account. A 404 means Logto no longer has it, which the
// caller tells apart through Status.
func (c *Client) User(ctx context.Context, userID string) (Member, error) {
	var u logtoUser
	if err := c.do(ctx, http.MethodGet, "/api/users/"+url.PathEscape(userID), nil, &u); err != nil {
		return Member{}, err
	}
	return u.member(c.cfg.AdminRole), nil
}

// Users lists every account in the tenant. A company is tens to hundreds of
// people, so this is a handful of pages an hour.
func (c *Client) Users(ctx context.Context) ([]Member, error) {
	var out []Member
	for page := 1; ; page++ {
		var batch []logtoUser
		path := fmt.Sprintf("/api/users?page=%d&page_size=%d", page, pageSize)
		if err := c.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, u := range batch {
			out = append(out, u.member(c.cfg.AdminRole))
		}
		if len(batch) < pageSize {
			return out, nil
		}
	}
}

// SetSuspended suspends or reactivates an account. Logto refuses a
// suspended account's next sign-in; pp-hub's own sessions are ended locally.
func (c *Client) SetSuspended(ctx context.Context, userID string, suspended bool) error {
	return c.do(ctx, http.MethodPatch, "/api/users/"+url.PathEscape(userID)+"/is-suspended",
		map[string]any{"isSuspended": suspended}, nil)
}

func (c *Client) AddMember(ctx context.Context, orgID, userID string) error {
	return c.do(ctx, http.MethodPost, "/api/organizations/"+url.PathEscape(orgID)+"/users",
		map[string]any{"userIds": []string{userID}}, nil)
}

func (c *Client) RemoveMember(ctx context.Context, orgID, userID string) error {
	return c.do(ctx, http.MethodDelete,
		"/api/organizations/"+url.PathEscape(orgID)+"/users/"+url.PathEscape(userID), nil, nil)
}

// SetAdmin replaces the member's whole role set, because that is the only
// shape the Management API offers. With one role in the template that is the
// same operation; with more, a second role would be dropped — which is why
// the admin role is the only one pp-hub is allowed to know about.
func (c *Client) SetAdmin(ctx context.Context, orgID, userID string, admin bool) error {
	names := []string{}
	if admin {
		names = append(names, c.cfg.AdminRole)
	}
	return c.do(ctx, http.MethodPut,
		"/api/organizations/"+url.PathEscape(orgID)+"/users/"+url.PathEscape(userID)+"/roles",
		map[string]any{"organizationRoleNames": names}, nil)
}

// Invite creates an organization invitation and asks Logto to email it.
//
// messagePayload is what makes this one call rather than three: pass it and
// Logto renders the tenant's OrganizationInvitation template and sends it. A
// 501 back means no email connector is configured, which the caller reports
// as such — an invitation nobody receives is not an invitation.
//
// link fills the template's {{link}}. Logto substitutes only what it is
// given, so without it the email's call to action points nowhere. It needs no
// invitation id: acceptance happens at sign-in (AcceptInvitations), keyed on
// the verified address, so the link only has to get the invitee signed in.
func (c *Client) Invite(ctx context.Context, orgID, email string, admin bool, ttl time.Duration, link string) error {
	body := map[string]any{
		"organizationId": orgID,
		"invitee":        email,
		"expiresAt":      time.Now().Add(ttl).UnixMilli(),
		"messagePayload": map[string]any{"link": link},
	}
	if admin {
		roleID, err := c.adminRole(ctx)
		if err != nil {
			return err
		}
		body["organizationRoleIds"] = []string{roleID}
	}
	return c.do(ctx, http.MethodPost, "/api/organization-invitations", body, nil)
}

// Invitation is an organization invitation as Logto lists it, with the roles
// the invitee receives on acceptance.
type Invitation struct {
	ID                string `json:"id"`
	OrganizationID    string `json:"organizationId"`
	Invitee           string `json:"invitee"`
	Status            string `json:"status"`
	ExpiresAt         int64  `json:"expiresAt"`
	OrganizationRoles []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"organizationRoles"`
}

// PendingInvitations returns the invitations addressed to email that can
// still be accepted.
//
// Logto keeps accepted, revoked and expired invitations in the same list and
// flips an expired one's status lazily, so both the status and the deadline
// are checked here. The address is compared again for the same reason
// FindUserByEmail does: the filter is the upstream's, the decision is ours.
func (c *Client) PendingInvitations(ctx context.Context, email string) ([]Invitation, error) {
	var all []Invitation
	if err := c.do(ctx, http.MethodGet,
		"/api/organization-invitations?invitee="+url.QueryEscape(email), nil, &all); err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	out := all[:0]
	for _, inv := range all {
		if inv.Status == "Pending" && inv.ExpiresAt > now && strings.EqualFold(inv.Invitee, email) {
			out = append(out, inv)
		}
	}
	return out, nil
}

// AcceptInvitation marks an invitation accepted by userID, which is what
// makes Logto add them to the organization with the invitation's roles.
func (c *Client) AcceptInvitation(ctx context.Context, invitationID, userID string) error {
	return c.do(ctx, http.MethodPut,
		"/api/organization-invitations/"+url.PathEscape(invitationID)+"/status",
		map[string]any{"status": "Accepted", "acceptedUserId": userID}, nil)
}

// adminRole resolves the configured role name to its id, which only the
// invitation API needs. Cached: the organization template changes about as
// often as the schema does.
func (c *Client) adminRole(ctx context.Context) (string, error) {
	c.mu.Lock()
	cached := c.adminRoleID
	c.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	var roles []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.do(ctx, http.MethodGet,
		"/api/organization-roles?page=1&page_size="+strconv.Itoa(pageSize), nil, &roles); err != nil {
		return "", err
	}
	for _, role := range roles {
		if strings.EqualFold(role.Name, c.cfg.AdminRole) {
			c.mu.Lock()
			c.adminRoleID = role.ID
			c.mu.Unlock()
			return role.ID, nil
		}
	}
	return "", fmt.Errorf("organization template has no role %q: create it, or set PP_LOGTO_ORG_ROLE_ADMIN to one that exists", c.cfg.AdminRole)
}
