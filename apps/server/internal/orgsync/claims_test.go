package orgsync

import "testing"

// The organization_roles claim is a flat list of "<orgId>:<role>" strings
// shared across every organization the token covers. Attributing a role to
// the wrong organization is the failure mode worth ruling out: it is how a
// member of one company would become an administrator of another.
func TestClaimsAttributeRolesToTheRightOrganization(t *testing.T) {
	c := Claims{
		Organizations:     []string{"org_a", "org_b"},
		OrganizationRoles: []string{"org_a:admin", "org_b:member"},
	}
	got := c.admins("admin")
	if len(got) != 2 {
		t.Fatalf("membership map = %v, want two organizations", got)
	}
	if !got["org_a"] {
		t.Error("org_a: admin role was not applied")
	}
	if got["org_b"] {
		t.Error("org_b: member was treated as an admin")
	}
}

// A role naming an organization the `organizations` claim does not list is
// not a membership. Honouring it would seat somebody in a space the token
// never said they belonged to.
func TestClaimsIgnoreRolesWithoutMembership(t *testing.T) {
	c := Claims{
		Organizations:     []string{"org_a"},
		OrganizationRoles: []string{"org_ghost:admin"},
	}
	got := c.admins("admin")
	if _, present := got["org_ghost"]; present {
		t.Error("a role alone produced a membership")
	}
	if got["org_a"] {
		t.Error("org_a was made admin by another organization's role")
	}
}

// The admin role name is operator configuration, and Logto's own console is
// not case-consistent about role names. Matching must not hinge on it.
func TestClaimsMatchTheConfiguredRoleCaseInsensitively(t *testing.T) {
	c := Claims{
		Organizations:     []string{"org_a"},
		OrganizationRoles: []string{"org_a:Owner"},
	}
	if !c.admins("owner")["org_a"] {
		t.Error("role match was case-sensitive")
	}
	if c.admins("admin")["org_a"] {
		t.Error("a role that is not the configured one granted admin")
	}
}

// A token from a provider that has never heard of organizations decodes to
// nothing, and must reconcile nothing rather than revoking every membership.
func TestClaimsFromANonLogtoProviderAreEmpty(t *testing.T) {
	if got := (Claims{}).admins("admin"); len(got) != 0 {
		t.Errorf("empty claims produced %v", got)
	}
}
