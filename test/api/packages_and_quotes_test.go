package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/eandstravel/digitalservice/internal/testutil"
)

func TestPackages(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "pkgco")
	slug := fmt.Sprintf("starter-%d", testutil.Unique())

	var packageID string

	t.Run("create requires slug", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/packages", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]interface{}{"name": map[string]string{"en": "Starter"}},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create requires name", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/packages", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]interface{}{"slug": slug},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create requires superadmin", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/packages", testutil.ReqOpts{
			Token: tenant.AdminToken,
			Body:  map[string]interface{}{"slug": slug, "name": map[string]string{"en": "Starter"}},
		})
		if resp.Status != http.StatusForbidden {
			t.Errorf("want 403 for tenant-admin token, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create succeeds", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/packages", testutil.ReqOpts{
			Token: superadmin,
			Body: map[string]interface{}{
				"slug":     slug,
				"name":     map[string]string{"en": "Starter", "mn": "Эхлэл"},
				"tagline":  map[string]string{"en": "For small teams"},
				"price":    1500000,
				"currency": "MNT",
				"features": map[string][]string{"en": {"1 project", "Email support"}},
			},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		packageID, _ = resp.Data()["id"].(string)
		if packageID == "" {
			t.Fatal("expected created package id")
		}
		if active, _ := resp.Data()["is_active"].(bool); !active {
			t.Error("expected new package to be active")
		}
	})

	t.Run("platform catalog list includes it", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/packages?page=1&limit=50", testutil.ReqOpts{})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		found := false
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == packageID {
				found = true
			}
		}
		if !found {
			t.Error("expected created package in the platform catalog list")
		}
	})

	t.Run("not visible to a tenant before assignment", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/packages/"+slug, testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusNotFound {
			t.Errorf("want 404 before assignment, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("assign requires superadmin", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants/"+tenant.ID+"/packages", testutil.ReqOpts{
			Token: tenant.AdminToken,
			Body:  map[string]string{"package_id": packageID},
		})
		if resp.Status != http.StatusForbidden {
			t.Errorf("want 403 for tenant-admin token, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("assign to tenant", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants/"+tenant.ID+"/packages", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"package_id": packageID},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("duplicate assign conflicts", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants/"+tenant.ID+"/packages", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"package_id": packageID},
		})
		if resp.Status != http.StatusConflict {
			t.Errorf("want 409, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("platform admin sees the assignment", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+tenant.ID+"/packages", testutil.ReqOpts{})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		found := false
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == packageID {
				found = true
			}
		}
		if !found {
			t.Error("expected assigned package in the tenant's assignment list")
		}
	})

	t.Run("public get by slug resolves locale once assigned", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/packages/"+slug+"?lang=mn", testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		if name, _ := resp.Data()["name"].(string); name != "Эхлэл" {
			t.Errorf("want localized name %q, got %q", "Эхлэл", name)
		}
	})

	t.Run("public get by slug falls back to default locale", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/packages/"+slug+"?lang=ko", testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		if name, _ := resp.Data()["name"].(string); name != "Starter" {
			t.Errorf("want fallback name %q, got %q", "Starter", name)
		}
	})

	t.Run("public list includes it once assigned", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/packages?page=1&limit=50", testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		found := false
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == packageID {
				found = true
			}
		}
		if !found {
			t.Error("expected assigned package in the tenant's public list")
		}
	})

	t.Run("not visible from another (unassigned) tenant", func(t *testing.T) {
		otherTenant := testutil.NewTenant(t, app, superadmin, "otherpkgco")
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/packages/"+slug, testutil.ReqOpts{APIKey: otherTenant.APIKey})
		if resp.Status != http.StatusNotFound {
			t.Errorf("want 404 (not assigned to this tenant), got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("update (partial) requires superadmin", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/packages/"+packageID, testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]interface{}{"price": 1200000, "highlighted": true},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("update unknown id is not found", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/packages/"+dummyID, testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]interface{}{"price": 1},
		})
		if resp.Status != http.StatusNotFound {
			t.Errorf("want 404, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("platform get by id keeps full locale map", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/packages/"+packageID, testutil.ReqOpts{})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		name, _ := resp.Data()["name"].(map[string]interface{})
		if name["en"] != "Starter" || name["mn"] != "Эхлэл" {
			t.Errorf("want both locales intact, got %v", name)
		}
	})

	t.Run("unassign removes it from the tenant's storefront", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodDelete, "/api/v1/platform/tenants/"+tenant.ID+"/packages/"+packageID, testutil.ReqOpts{
			Token: superadmin,
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		getResp := testutil.Do(t, app, http.MethodGet, "/api/v1/packages/"+slug, testutil.ReqOpts{APIKey: tenant.APIKey})
		if getResp.Status != http.StatusNotFound {
			t.Errorf("want 404 after unassign, got %d: %s", getResp.Status, getResp.Raw)
		}
	})

	t.Run("delete removes it from the platform catalog", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodDelete, "/api/v1/platform/packages/"+packageID, testutil.ReqOpts{
			Token: superadmin,
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		getResp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/packages/"+packageID, testutil.ReqOpts{})
		if active, _ := getResp.Data()["is_active"].(bool); active {
			t.Error("expected package to be inactive after delete")
		}
	})
}

func TestQuotes(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "quoteco")

	var quoteID string

	t.Run("missing required fields rejected", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/quotes", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body:   map[string]string{"message": "Need a website"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create succeeds (public, no auth)", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/quotes", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body: map[string]string{
				"name":         "Nomin Batbayar",
				"email":        fmt.Sprintf("nomin-%d@example.com", testutil.Unique()),
				"company_name": "Nomad Retail LLC",
				"package_slug": "starter",
				"budget":       "$5,000-$10,000",
				"timeline":     "1-2 months",
				"message":      "Need a new storefront site.",
			},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		quoteID, _ = resp.Data()["id"].(string)
		if quoteID == "" {
			t.Fatal("expected created quote id")
		}
		if status, _ := resp.Data()["status"].(string); status != "new" {
			t.Errorf("want initial status new, got %q", status)
		}
	})

	t.Run("admin list/update-status", func(t *testing.T) {
		listResp := testutil.Do(t, app, http.MethodGet, "/api/v1/admin/quotes?page=1&limit=50", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
		})
		if listResp.Status != http.StatusOK {
			t.Fatalf("list: want 200, got %d: %s", listResp.Status, listResp.Raw)
		}

		updResp := testutil.Do(t, app, http.MethodPut, "/api/v1/admin/quotes/"+quoteID+"/status", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]string{"status": "contacted"},
		})
		if updResp.Status != http.StatusOK {
			t.Fatalf("update: want 200, got %d: %s", updResp.Status, updResp.Raw)
		}
	})

	t.Run("not visible from another tenant's admin list", func(t *testing.T) {
		otherTenant := testutil.NewTenant(t, app, superadmin, "otherquoteco")
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/admin/quotes?page=1&limit=50", testutil.ReqOpts{
			Token: otherTenant.AdminToken, APIKey: otherTenant.APIKey,
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == quoteID {
				t.Error("expected tenant isolation: other tenant should not see this quote")
			}
		}
	})

	t.Run("platform admin sees it in the consolidated list", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/quotes?page=1&limit=50", testutil.ReqOpts{})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		found := false
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == quoteID {
				found = true
			}
		}
		if !found {
			t.Error("expected tenant-linked quote in the platform-wide list")
		}
	})

	var tenantlessQuoteID string

	t.Run("create with no tenant (platform-wide, fully public)", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/quotes", testutil.ReqOpts{
			Body: map[string]string{
				"name":    "Prospective Client",
				"email":   fmt.Sprintf("prospect-%d@example.com", testutil.Unique()),
				"message": "Interested in becoming a client — no existing account.",
			},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		tenantlessQuoteID, _ = resp.Data()["id"].(string)
		if tenantlessQuoteID == "" {
			t.Fatal("expected created quote id")
		}
		if _, ok := resp.Data()["tenant_id"]; ok {
			t.Errorf("expected no tenant_id on a tenant-less quote, got %v", resp.Data()["tenant_id"])
		}
	})

	t.Run("create with no tenant still requires name and email", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/quotes", testutil.ReqOpts{
			Body: map[string]string{"message": "Missing name and email."},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("tenant-less quote appears in the platform-wide list", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/quotes?page=1&limit=50", testutil.ReqOpts{})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		found := false
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == tenantlessQuoteID {
				found = true
			}
		}
		if !found {
			t.Error("expected tenant-less quote in the platform-wide list")
		}
	})

	t.Run("platform status update requires superadmin", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/quotes/"+tenantlessQuoteID+"/status", testutil.ReqOpts{
			Token: tenant.AdminToken,
			Body:  map[string]string{"status": "contacted"},
		})
		if resp.Status != http.StatusForbidden {
			t.Errorf("want 403 for tenant-admin token, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("platform status update works on a tenant-less quote", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/quotes/"+tenantlessQuoteID+"/status", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"status": "contacted"},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})
}
