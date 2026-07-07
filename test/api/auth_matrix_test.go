// Package api holds black-box integration tests that exercise the full HTTP
// stack (real router, real auth middleware, real MongoDB via testcontainers)
// rather than testing handlers/services in isolation.
package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/testutil"
)

// dummyID is a syntactically valid ObjectID used in :id path segments for
// auth-boundary tests, where the request should be rejected by middleware
// before the handler ever tries to resolve the id.
const dummyID = "0123456789abcdef01234567"

type routeCase struct {
	method string
	path   string
	body   interface{}
}

var adminGatedRoutes = []routeCase{
	{http.MethodPost, "/api/v1/admin/users", map[string]string{"email": "x@x.com"}},
	{http.MethodGet, "/api/v1/admin/users", nil},
	{http.MethodPut, "/api/v1/admin/users/" + dummyID + "/status", map[string]string{"status": "suspended"}},
	{http.MethodPost, "/api/v1/admin/destinations", map[string]string{"slug": "x"}},
	{http.MethodPut, "/api/v1/admin/destinations/" + dummyID, map[string]string{}},
	{http.MethodDelete, "/api/v1/admin/destinations/" + dummyID, nil},
	{http.MethodPost, "/api/v1/admin/blogs", map[string]string{"slug": "x"}},
	{http.MethodPut, "/api/v1/admin/blogs/" + dummyID, map[string]string{}},
	{http.MethodPost, "/api/v1/admin/blogs/" + dummyID + "/publish", nil},
	{http.MethodGet, "/api/v1/admin/bookings", nil},
	{http.MethodGet, "/api/v1/admin/bookings/" + dummyID, nil},
	{http.MethodPut, "/api/v1/admin/bookings/" + dummyID + "/status", map[string]string{"status": "confirmed"}},
	{http.MethodPost, "/api/v1/admin/cars", map[string]string{"slug": "x"}},
	{http.MethodPut, "/api/v1/admin/cars/" + dummyID, map[string]string{}},
	{http.MethodDelete, "/api/v1/admin/cars/" + dummyID, nil},
	{http.MethodGet, "/api/v1/admin/rentals", nil},
	{http.MethodGet, "/api/v1/admin/rentals/" + dummyID, nil},
	{http.MethodPut, "/api/v1/admin/rentals/" + dummyID + "/status", map[string]string{"status": "confirmed"}},
	{http.MethodGet, "/api/v1/admin/airport-transfers", nil},
	{http.MethodGet, "/api/v1/admin/airport-transfers/" + dummyID, nil},
	{http.MethodPut, "/api/v1/admin/airport-transfers/" + dummyID + "/status", map[string]string{"status": "confirmed"}},
	{http.MethodGet, "/api/v1/admin/contact-messages", nil},
	{http.MethodPut, "/api/v1/admin/contact-messages/" + dummyID + "/status", map[string]string{"status": "read"}},
}

var platformGatedRoutes = []routeCase{
	{http.MethodPost, "/api/v1/platform/admins", map[string]string{"email": "x@x.com"}},
	{http.MethodGet, "/api/v1/platform/admins", nil},
	{http.MethodPut, "/api/v1/platform/admins/" + dummyID + "/status", map[string]string{"status": "suspended"}},
	{http.MethodPost, "/api/v1/platform/tenants", map[string]string{"slug": "x"}},
	{http.MethodGet, "/api/v1/platform/tenants", nil},
	{http.MethodGet, "/api/v1/platform/tenants/" + dummyID, nil},
	{http.MethodPut, "/api/v1/platform/tenants/" + dummyID + "/status", map[string]string{"status": "suspended"}},
	{http.MethodPost, "/api/v1/platform/tenants/" + dummyID + "/rotate-key", nil},
	{http.MethodPost, "/api/v1/platform/tenants/" + dummyID + "/subscription", map[string]string{"plan": "free"}},
	{http.MethodGet, "/api/v1/platform/tenants/" + dummyID + "/subscription", nil},
	{http.MethodPut, "/api/v1/platform/tenants/" + dummyID + "/subscription/plan", map[string]string{"plan": "pro"}},
	{http.MethodPost, "/api/v1/platform/tenants/" + dummyID + "/subscription/cancel", nil},
}

func TestAdminGatedRoutes_RejectMissingAndWrongAuth(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)

	superadmin := testutil.SuperadminToken(t, app)
	tenantA := testutil.NewTenant(t, app, superadmin, "tenanta")
	tenantB := testutil.NewTenant(t, app, superadmin, "tenantb")
	_, staffToken := tenantA.CreateStaff(t, app, "staffer")

	for _, rc := range adminGatedRoutes {
		rc := rc
		t.Run(rc.method+" "+rc.path+"/no-token", func(t *testing.T) {
			resp := testutil.Do(t, app, rc.method, rc.path, testutil.ReqOpts{APIKey: tenantA.APIKey, Body: rc.body})
			if resp.Status != http.StatusUnauthorized {
				t.Errorf("want 401 with no token, got %d: %s", resp.Status, resp.Raw)
			}
		})
		t.Run(rc.method+" "+rc.path+"/wrong-role", func(t *testing.T) {
			resp := testutil.Do(t, app, rc.method, rc.path, testutil.ReqOpts{Token: staffToken, APIKey: tenantA.APIKey, Body: rc.body})
			if resp.Status != http.StatusForbidden {
				t.Errorf("want 403 for staff-role token, got %d: %s", resp.Status, resp.Raw)
			}
		})
		t.Run(rc.method+" "+rc.path+"/cross-tenant-token", func(t *testing.T) {
			// tenantB's admin token, replayed against tenantA's API key.
			resp := testutil.Do(t, app, rc.method, rc.path, testutil.ReqOpts{Token: tenantB.AdminToken, APIKey: tenantA.APIKey, Body: rc.body})
			if resp.Status != http.StatusForbidden {
				t.Errorf("want 403 for cross-tenant token, got %d: %s", resp.Status, resp.Raw)
			}
		})
	}
}

func TestPlatformGatedRoutes_RejectMissingAndWrongAuth(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)

	superadmin := testutil.SuperadminToken(t, app)
	tenantA := testutil.NewTenant(t, app, superadmin, "tenanta")

	for _, rc := range platformGatedRoutes {
		rc := rc
		t.Run(rc.method+" "+rc.path+"/no-token", func(t *testing.T) {
			resp := testutil.Do(t, app, rc.method, rc.path, testutil.ReqOpts{Body: rc.body})
			if resp.Status != http.StatusUnauthorized {
				t.Errorf("want 401 with no token, got %d: %s", resp.Status, resp.Raw)
			}
		})
		t.Run(rc.method+" "+rc.path+"/wrong-role", func(t *testing.T) {
			// A legitimate tenant-admin token must never open a platform route.
			resp := testutil.Do(t, app, rc.method, rc.path, testutil.ReqOpts{Token: tenantA.AdminToken, Body: rc.body})
			if resp.Status != http.StatusForbidden {
				t.Errorf("want 403 for tenant-admin token, got %d: %s", resp.Status, resp.Raw)
			}
		})
	}
}

// TestForgedSuperadminTokenWithTenantScopeRejected is a regression test for
// the privilege-escalation bug where a "superadmin" JWT claim carrying a
// non-empty tenant_id was accepted on platform routes, because those routes
// aren't behind TenantMiddleware and so never populated "tenant_id" in the
// gin context for AuthMiddleware to cross-check against. AuthMiddleware.Require
// now rejects any superadmin claim that carries a tenant scope outright,
// regardless of route. This test mints that exact shape of token directly
// (bypassing the role whitelist in TenantUserService.Create, which is the
// other half of the fix) to make sure the middleware itself still holds the
// line even if a bad role value ever got into a token some other way.
func TestForgedSuperadminTokenWithTenantScopeRejected(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)

	forged, _, err := app.Maker.CreateToken(dummyID, "", dummyID, time.Hour)
	if err != nil {
		t.Fatalf("mint forged token: %v", err)
	}

	resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants", testutil.ReqOpts{Token: forged})
	if resp.Status != http.StatusForbidden {
		t.Fatalf("tenant token: want 403, got %d: %s", resp.Status, resp.Raw)
	}
}

// TestTenantUserCreateRejectsSuperadminRole is a regression test for the
// other half of the same fix: TenantUserService.Create must reject any role
// value outside {admin, staff}, so a tenant admin can never provision a login
// profile that would log in with a platform-level token.
func TestTenantUserCreateRejectsSuperadminRole(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)

	superadmin := testutil.SuperadminToken(t, app)
	tenantA := testutil.NewTenant(t, app, superadmin, "tenanta")

	resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/users", testutil.ReqOpts{
		Token:  tenantA.AdminToken,
		APIKey: tenantA.APIKey,
		Body:   map[string]string{"email": "", "role": ""},
	})
	if resp.Status != http.StatusBadRequest {
		t.Fatalf("want 400 invalid role, got %d: %s", resp.Status, resp.Raw)
	}
	if resp.Message() != "invalid role" {
		t.Errorf("want message %q, got %q", "invalid role", resp.Message())
	}
}
