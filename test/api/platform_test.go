package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/eandstravel/digitalservice/internal/testutil"
)

func TestPlatformLogin(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)

	t.Run("success", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/login", testutil.ReqOpts{
			Body: map[string]string{"email": testutil.SuperadminEmail, "password": testutil.SuperadminPassword},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		if tok, _ := resp.Data()["token"].(string); tok == "" {
			t.Error("expected a non-empty token")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/login", testutil.ReqOpts{
			Body: map[string]string{"email": testutil.SuperadminEmail, "password": "not-the-password"},
		})
		if resp.Status != http.StatusUnauthorized {
			t.Errorf("want 401, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("unknown email", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/login", testutil.ReqOpts{
			Body: map[string]string{"email": "nobody@test.local", "password": "whatever123"},
		})
		if resp.Status != http.StatusUnauthorized {
			t.Errorf("want 401, got %d: %s", resp.Status, resp.Raw)
		}
	})
}

func TestPlatformAdminUsers(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)

	var createdID string

	t.Run("create with auto-generated password", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/admins", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"name": "Second Admin", "email": "second-admin@test.local"},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		if pw, _ := resp.Data()["password"].(string); pw == "" {
			t.Error("expected an auto-generated password to be returned")
		}
		user, _ := resp.Data()["user"].(map[string]interface{})
		createdID, _ = user["id"].(string)
		if createdID == "" {
			t.Fatal("expected created user id")
		}
	})

	t.Run("duplicate email conflicts", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/admins", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"name": "Dup", "email": "second-admin@test.local"},
		})
		if resp.Status != http.StatusConflict {
			t.Errorf("want 409, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("list includes created user", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/admins?page=1&limit=50", testutil.ReqOpts{Token: superadmin})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		found := false
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == createdID {
				found = true
			}
		}
		if !found {
			t.Error("expected created user to appear in list")
		}
	})

	t.Run("suspend then reactivate", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/admins/"+createdID+"/status", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"status": "suspended"},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		resp = testutil.Do(t, app, http.MethodPut, "/api/v1/platform/admins/"+createdID+"/status", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"status": "active"},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("reactivate: want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("cannot suspend the last active platform user", func(t *testing.T) {
		// Suspend the second admin, then try to suspend the bootstrapped
		// superadmin too - only one active user remains at that point.
		testutil.Do(t, app, http.MethodPut, "/api/v1/platform/admins/"+createdID+"/status", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"status": "suspended"},
		})

		loginResp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/login", testutil.ReqOpts{
			Body: map[string]string{"email": testutil.SuperadminEmail, "password": testutil.SuperadminPassword},
		})
		selfID, _ := loginResp.Data()["token"].(string)
		_ = selfID // token itself, not an id; fetch id via list instead
		listResp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/admins?page=1&limit=50", testutil.ReqOpts{Token: superadmin})
		items, _ := listResp.Body["data"].([]interface{})
		var bootstrapID string
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if email, _ := m["email"].(string); email == testutil.SuperadminEmail {
				bootstrapID, _ = m["id"].(string)
			}
		}
		if bootstrapID == "" {
			t.Fatal("could not find bootstrapped superadmin in list")
		}

		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/admins/"+bootstrapID+"/status", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"status": "suspended"},
		})
		if resp.Status != http.StatusConflict {
			t.Errorf("want 409 (cannot suspend last active user), got %d: %s", resp.Status, resp.Raw)
		}
	})
}

func TestPlatformTenants(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)

	var tenantID, apiKey string

	t.Run("create without contact_email", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"name": "No Contact Co", "slug": fmt.Sprintf("no-contact-%d", testutil.Unique())},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		data := resp.Data()
		if _, hasLogin := data["login"]; hasLogin {
			t.Error("expected no bootstrap login profile when contact_email is omitted")
		}
		if key, _ := data["api_key"].(string); key == "" {
			t.Error("expected an api_key to be returned")
		}
		tenantObj, _ := data["tenant"].(map[string]interface{})
		tenantID, _ = tenantObj["id"].(string)
		apiKey, _ = data["api_key"].(string)
	})

	t.Run("missing slug rejected", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"name": "No Slug Co"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("get by id", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+tenantID, testutil.ReqOpts{Token: superadmin})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("list includes created tenant", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants?page=1&limit=100", testutil.ReqOpts{Token: superadmin})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		found := false
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == tenantID {
				found = true
			}
		}
		if !found {
			t.Error("expected created tenant to appear in list")
		}
	})

	t.Run("suspend blocks tenant-scoped requests", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+tenantID+"/status", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"status": "suspended"},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		blocked := testutil.Do(t, app, http.MethodGet, "/api/v1/destinations", testutil.ReqOpts{APIKey: apiKey})
		if blocked.Status != http.StatusForbidden {
			t.Errorf("want 403 for suspended tenant, got %d: %s", blocked.Status, blocked.Raw)
		}

		// reactivate for the next subtest
		testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+tenantID+"/status", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"status": "active"},
		})
	})

	t.Run("rotate api key invalidates the old one", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants/"+tenantID+"/rotate-key", testutil.ReqOpts{Token: superadmin})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		newKey, _ := resp.Data()["api_key"].(string)
		if newKey == "" || newKey == apiKey {
			t.Fatalf("expected a new, different api_key, got %q (old was %q)", newKey, apiKey)
		}

		oldKeyResp := testutil.Do(t, app, http.MethodGet, "/api/v1/destinations", testutil.ReqOpts{APIKey: apiKey})
		if oldKeyResp.Status != http.StatusUnauthorized {
			t.Errorf("want 401 for rotated-out old key, got %d: %s", oldKeyResp.Status, oldKeyResp.Raw)
		}

		newKeyResp := testutil.Do(t, app, http.MethodGet, "/api/v1/destinations", testutil.ReqOpts{APIKey: newKey})
		if newKeyResp.Status != http.StatusOK {
			t.Errorf("want 200 for new key, got %d: %s", newKeyResp.Status, newKeyResp.Raw)
		}
	})

	t.Run("create with contact_email bootstraps an admin login", func(t *testing.T) {
		contactEmail := fmt.Sprintf("ops-%d@tenant.test", testutil.Unique())
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants", testutil.ReqOpts{
			Token: superadmin,
			Body: map[string]string{
				"name":          "Contact Co",
				"slug":          fmt.Sprintf("contact-co-%d", testutil.Unique()),
				"contact_email": contactEmail,
			},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		login, _ := resp.Data()["login"].(map[string]interface{})
		if login == nil {
			t.Fatal("expected a bootstrap login object")
		}
		if pw, _ := login["password"].(string); pw == "" {
			t.Error("expected a one-time password for the bootstrap admin")
		}
	})
}
