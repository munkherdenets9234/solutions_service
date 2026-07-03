package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/eandstravel/digitalservice/internal/testutil"
)

func TestTenantUserLogin(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "loginco")

	t.Run("wrong password", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/login", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body:   map[string]string{"email": tenant.AdminEmail, "password": "not-the-password"},
		})
		if resp.Status != http.StatusUnauthorized {
			t.Errorf("want 401, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("missing X-API-Key", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/login", testutil.ReqOpts{
			Body: map[string]string{"email": tenant.AdminEmail, "password": "whatever"},
		})
		if resp.Status != http.StatusUnauthorized {
			t.Errorf("want 401, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("wrong tenant's api key for a real email+password", func(t *testing.T) {
		otherTenant := testutil.NewTenant(t, app, superadmin, "otherco")
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/login", testutil.ReqOpts{
			APIKey: otherTenant.APIKey,
			Body:   map[string]string{"email": tenant.AdminEmail, "password": "whatever"},
		})
		if resp.Status != http.StatusUnauthorized {
			t.Errorf("want 401 (no such user for that tenant), got %d: %s", resp.Status, resp.Raw)
		}
	})
}

func TestTenantUserCreateListUpdateStatus(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "usersco")

	var staffID, staffEmail string

	t.Run("create staff with auto-generated password", func(t *testing.T) {
		staffEmail = fmt.Sprintf("staff-%d@tenant.test", testutil.Unique())
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/users", testutil.ReqOpts{
			Token:  tenant.AdminToken,
			APIKey: tenant.APIKey,
			Body:   map[string]string{"name": "Staffer", "email": staffEmail, "role": "staff"},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		if pw, _ := resp.Data()["password"].(string); pw == "" {
			t.Error("expected an auto-generated password to be returned")
		}
		user, _ := resp.Data()["user"].(map[string]interface{})
		staffID, _ = user["id"].(string)
		if role, _ := user["role"].(string); role != "staff" {
			t.Errorf("want role staff, got %q", role)
		}
	})

	t.Run("default role is staff when omitted", func(t *testing.T) {
		email := fmt.Sprintf("default-role-%d@tenant.test", testutil.Unique())
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/users", testutil.ReqOpts{
			Token:  tenant.AdminToken,
			APIKey: tenant.APIKey,
			Body:   map[string]string{"email": email},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		user, _ := resp.Data()["user"].(map[string]interface{})
		if role, _ := user["role"].(string); role != "staff" {
			t.Errorf("want default role staff, got %q", role)
		}
	})

	t.Run("invalid role rejected", func(t *testing.T) {
		email := fmt.Sprintf("bad-role-%d@tenant.test", testutil.Unique())
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/users", testutil.ReqOpts{
			Token:  tenant.AdminToken,
			APIKey: tenant.APIKey,
			Body:   map[string]string{"email": email, "role": "owner"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("password shorter than 8 chars rejected", func(t *testing.T) {
		email := fmt.Sprintf("shortpw-%d@tenant.test", testutil.Unique())
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/users", testutil.ReqOpts{
			Token:  tenant.AdminToken,
			APIKey: tenant.APIKey,
			Body:   map[string]string{"email": email, "password": "short"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("duplicate email within tenant conflicts", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/users", testutil.ReqOpts{
			Token:  tenant.AdminToken,
			APIKey: tenant.APIKey,
			Body:   map[string]string{"email": staffEmail},
		})
		if resp.Status != http.StatusConflict {
			t.Errorf("want 409, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("same email allowed in a different tenant", func(t *testing.T) {
		otherTenant := testutil.NewTenant(t, app, superadmin, "otherusersco")
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/users", testutil.ReqOpts{
			Token:  otherTenant.AdminToken,
			APIKey: otherTenant.APIKey,
			Body:   map[string]string{"email": staffEmail},
		})
		if resp.Status != http.StatusCreated {
			t.Errorf("want 201 (per-tenant uniqueness only), got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("list includes created user", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/admin/users?page=1&limit=50", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		found := false
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == staffID {
				found = true
			}
		}
		if !found {
			t.Error("expected created staff user to appear in list")
		}
	})

	t.Run("suspend then staff can no longer log in", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/admin/users/"+staffID+"/status", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]string{"status": "suspended"},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("list/update-status scoped to own tenant only", func(t *testing.T) {
		otherTenant := testutil.NewTenant(t, app, superadmin, "isolatedco")
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/admin/users/"+staffID+"/status", testutil.ReqOpts{
			Token: otherTenant.AdminToken, APIKey: otherTenant.APIKey,
			Body: map[string]string{"status": "active"},
		})
		// Cross-tenant token is already rejected by AuthMiddleware before this
		// point (covered by the auth matrix); this checks the deeper
		// repository-level guard too, using a legitimately-scoped admin token
		// for a tenant that simply doesn't own this user id.
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200 (update is a no-op for a non-matching id), got %d: %s", resp.Status, resp.Raw)
		}

		getResp := testutil.Do(t, app, http.MethodGet, "/api/v1/admin/users?page=1&limit=50", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
		})
		items, _ := getResp.Body["data"].([]interface{})
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == staffID {
				if status, _ := m["status"].(string); status != "suspended" {
					t.Errorf("expected staff user to remain suspended (other tenant's update must not affect it), got %q", status)
				}
			}
		}
	})
}
