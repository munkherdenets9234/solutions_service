package api

import (
	"net/http"
	"testing"

	"github.com/eandstravel/digitalservice/internal/testutil"
)

func TestTenantProjectShowcase(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	shown := testutil.NewTenant(t, app, superadmin, "showcaseco")
	hidden := testutil.NewTenant(t, app, superadmin, "hiddenco")

	t.Run("not showcased by default", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/projects/"+hidden.Slug, testutil.ReqOpts{})
		if resp.Status != http.StatusNotFound {
			t.Errorf("want 404 before opting in, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("update project requires superadmin", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+shown.ID+"/project", testutil.ReqOpts{
			Token: shown.AdminToken,
			Body:  map[string]interface{}{"showcase": true},
		})
		if resp.Status != http.StatusForbidden {
			t.Errorf("want 403 for tenant-admin token, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("superadmin enables the showcase", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+shown.ID+"/project", testutil.ReqOpts{
			Token: superadmin,
			Body: map[string]interface{}{
				"tagline":     map[string]string{"en": "A modern storefront", "mn": "Орчин үеийн дэлгүүр"},
				"description": map[string]string{"en": "Full case study."},
				"category":    "E-commerce",
				"website_url": "https://nomadtrails.example.com",
				"admin_cover": map[string]string{"url": "https://cdn.example.com/admin-cover.jpg", "caption": "Homepage grid image"},
				"metrics":     []map[string]interface{}{{"label": map[string]string{"en": "Faster checkout"}, "value": "40%"}},
				"showcase":    true,
				"featured":    true,
				"sort_order":  1,
			},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("public detail resolves locale and includes website_url/admin_cover", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/projects/"+shown.Slug+"?lang=mn", testutil.ReqOpts{})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		if tagline, _ := resp.Data()["tagline"].(string); tagline != "Орчин үеийн дэлгүүр" {
			t.Errorf("want localized tagline, got %q", tagline)
		}
		metrics, _ := resp.Data()["metrics"].([]interface{})
		if len(metrics) != 1 {
			t.Fatalf("want 1 metric, got %d", len(metrics))
		}
		if liveURL, _ := resp.Data()["live_url"].(string); liveURL != "https://nomadtrails.example.com" {
			t.Errorf("want live_url from website_url, got %q", liveURL)
		}
		adminCover, _ := resp.Data()["admin_cover"].(map[string]interface{})
		if adminCover["url"] != "https://cdn.example.com/admin-cover.jpg" {
			t.Errorf("want admin_cover url, got %v", adminCover)
		}
	})

	t.Run("partial update does not clobber unrelated fields", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+shown.ID+"/project", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]interface{}{"website_url": "https://updated.example.com"},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		getResp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+shown.ID, testutil.ReqOpts{})
		project, _ := getResp.Data()["project"].(map[string]interface{})
		if project["category"] != "E-commerce" {
			t.Errorf("want category left intact by an unrelated partial update, got %v", project["category"])
		}
		tagline, _ := project["tagline"].(map[string]interface{})
		if tagline["en"] != "A modern storefront" {
			t.Errorf("want tagline left intact by an unrelated partial update, got %v", tagline)
		}
		if project["website_url"] != "https://updated.example.com" {
			t.Errorf("want website_url updated, got %v", project["website_url"])
		}
	})

	t.Run("locale fields support dot-notation partial update", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+shown.ID+"/project", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]interface{}{"tagline.mn": "Шинэчилсэн"},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		getResp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+shown.ID, testutil.ReqOpts{})
		project, _ := getResp.Data()["project"].(map[string]interface{})
		tagline, _ := project["tagline"].(map[string]interface{})
		if tagline["mn"] != "Шинэчилсэн" {
			t.Errorf("want mn tagline updated via dot notation, got %v", tagline)
		}
		if tagline["en"] != "A modern storefront" {
			t.Errorf("want en tagline left intact, got %v", tagline)
		}
	})

	t.Run("public list includes the showcased tenant, not the hidden one", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/projects?page=1&limit=50", testutil.ReqOpts{})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		var sawShown, sawHidden bool
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			switch m["id"] {
			case shown.ID:
				sawShown = true
			case hidden.ID:
				sawHidden = true
			}
		}
		if !sawShown {
			t.Error("expected showcased tenant in public project list")
		}
		if sawHidden {
			t.Error("expected non-showcased tenant to be excluded")
		}
	})

	t.Run("existing tenant management read is unaffected", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+hidden.ID, testutil.ReqOpts{})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		if _, ok := resp.Data()["contact_email"]; !ok {
			t.Error("expected /platform/tenants/:id to still return full tenant detail")
		}
		if resp.Data()["project"] != nil {
			t.Error("expected no embedded project for a tenant with no TenantDetail record")
		}
	})

	t.Run("tenant detail is embedded as project on GET /platform/tenants/{id}", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+shown.ID, testutil.ReqOpts{})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		project, _ := resp.Data()["project"].(map[string]interface{})
		if project == nil {
			t.Fatal("expected embedded project for a tenant with a TenantDetail record")
		}
		category, _ := project["category"].(string)
		if category != "E-commerce" {
			t.Errorf("want embedded project category %q, got %q", "E-commerce", category)
		}
		tagline, _ := project["tagline"].(map[string]interface{})
		if tagline["en"] != "A modern storefront" {
			t.Errorf("want full locale map (admin-facing, unresolved), got %v", tagline)
		}
	})

	t.Run("tenant detail is embedded as project on GET /platform/tenants (list)", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants?page=1&limit=50", testutil.ReqOpts{Token: superadmin})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		var found bool
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if m["id"] != shown.ID {
				continue
			}
			found = true
			if m["project"] == nil {
				t.Error("expected embedded project in the tenants list for the showcased tenant")
			}
		}
		if !found {
			t.Fatal("expected shown tenant in the list")
		}
	})
}
