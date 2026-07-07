package testutil

import (
	"fmt"
	"net/http"
	"testing"
)

// Superadmin bootstrap credentials used by every test app (see
// PlatformUserService.EnsureBootstrap, wired in NewApp). Generated per test
// binary run rather than hardcoded, since they only ever exist against a
// throwaway test database.
var (
	SuperadminName     = "Test Superadmin"
	SuperadminEmail    = "superadmin-" + randomHex(4) + "@test.local"
	SuperadminPassword = randomHex(16)
)

// SuperadminToken logs in as the bootstrapped superadmin and returns its
// bearer token.
func SuperadminToken(t testing.TB, app *App) string {
	t.Helper()
	resp := Do(t, app, http.MethodPost, "/api/v1/platform/login", ReqOpts{
		Body: map[string]string{"email": SuperadminEmail, "password": SuperadminPassword},
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("superadmin login failed: %d %s", resp.Status, resp.Raw)
	}
	tok, _ := resp.Data()["token"].(string)
	if tok == "" {
		t.Fatalf("superadmin login returned no token: %s", resp.Raw)
	}
	return tok
}

// Tenant bundles everything a test needs to act as one fully provisioned
// tenant: its API key and a logged-in admin token.
type Tenant struct {
	ID         string
	APIKey     string
	AdminEmail string
	AdminToken string
}

// NewTenant provisions a fresh tenant (with a unique slug/contact email so
// parallel tests don't collide), logging in as its bootstrapped admin.
func NewTenant(t testing.TB, app *App, superadminToken, namePrefix string) Tenant {
	t.Helper()

	unique := fmt.Sprintf("%s-%d", namePrefix, timeNowUnixNano())
	contactEmail := unique + "@tenant.test"

	resp := Do(t, app, http.MethodPost, "/api/v1/platform/tenants", ReqOpts{
		Token: superadminToken,
		Body: map[string]string{
			"name":          unique,
			"slug":          unique,
			"contact_email": contactEmail,
		},
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("create tenant failed: %d %s", resp.Status, resp.Raw)
	}

	data := resp.Data()
	tenantObj, _ := data["tenant"].(map[string]interface{})
	tenantID, _ := tenantObj["id"].(string)
	apiKey, _ := data["api_key"].(string)
	loginObj, _ := data["login"].(map[string]interface{})
	adminPassword, _ := loginObj["password"].(string)

	if tenantID == "" || apiKey == "" || adminPassword == "" {
		t.Fatalf("create tenant response missing expected fields: %s", resp.Raw)
	}

	loginResp := Do(t, app, http.MethodPost, "/api/v1/login", ReqOpts{
		APIKey: apiKey,
		Body:   map[string]string{"email": contactEmail, "password": adminPassword},
	})
	if loginResp.Status != http.StatusOK {
		t.Fatalf("tenant admin login failed: %d %s", loginResp.Status, loginResp.Raw)
	}
	adminToken, _ := loginResp.Data()["token"].(string)
	if adminToken == "" {
		t.Fatalf("tenant admin login returned no token: %s", loginResp.Raw)
	}

	return Tenant{ID: tenantID, APIKey: apiKey, AdminEmail: contactEmail, AdminToken: adminToken}
}

// CreateStaff adds a staff-role login profile to the tenant and logs in as
// them, for tests that need a token with the (weaker) "staff" role.
func (tn Tenant) CreateStaff(t testing.TB, app *App, namePrefix string) (email, token string) {
	t.Helper()

	email = fmt.Sprintf("%s-%d@tenant.test", namePrefix, timeNowUnixNano())
	password := randomHex(12)
	createResp := Do(t, app, http.MethodPost, "/api/v1/admin/users", ReqOpts{
		Token:  tn.AdminToken,
		APIKey: tn.APIKey,
		Body:   map[string]string{"email": email, "password": password},
	})
	if createResp.Status != http.StatusCreated {
		t.Fatalf("create staff user failed: %d %s", createResp.Status, createResp.Raw)
	}

	loginResp := Do(t, app, http.MethodPost, "/api/v1/login", ReqOpts{
		APIKey: tn.APIKey,
		Body:   map[string]string{"email": email, "password": password},
	})
	if loginResp.Status != http.StatusOK {
		t.Fatalf("staff login failed: %d %s", loginResp.Status, loginResp.Raw)
	}
	tok, _ := loginResp.Data()["token"].(string)
	if tok == "" {
		t.Fatalf("staff login returned no token: %s", loginResp.Raw)
	}
	return email, tok
}
