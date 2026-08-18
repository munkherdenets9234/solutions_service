package api

import (
	"net/http"
	"testing"

	"github.com/eandstravel/digitalservice/internal/testutil"
)

func TestTenantReviews(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "reviewedco")

	var reviewID string

	t.Run("create requires tenant_id", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/reviews", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]interface{}{"rate": 5, "comment": map[string]string{"en": "Great work"}, "company_name": "Acme"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create rejects unknown tenant_id", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/reviews", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]interface{}{"tenant_id": dummyID, "rate": 5, "comment": map[string]string{"en": "Great work"}, "company_name": "Acme"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create rejects out-of-range rate", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/reviews", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]interface{}{"tenant_id": tenant.ID, "rate": 6, "comment": map[string]string{"en": "Great work"}, "company_name": "Acme"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create succeeds", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/reviews", testutil.ReqOpts{
			Token: superadmin,
			Body: map[string]interface{}{
				"tenant_id":          tenant.ID,
				"rate":               5,
				"comment":            map[string]string{"en": "Inno Nomads shipped our site faster than we expected.", "mn": "Инно Номадс манай сайтыг хүлээснээс хурдан гаргаж өгсөн."},
				"company_name":       "Nomad Retail LLC",
				"company_owner_name": "Bat-Erdene O.",
			},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		reviewID, _ = resp.Data()["id"].(string)
		if reviewID == "" {
			t.Fatal("expected created review id")
		}
	})

	t.Run("create requires superadmin", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/reviews", testutil.ReqOpts{
			Token: tenant.AdminToken,
			Body:  map[string]interface{}{"tenant_id": tenant.ID, "rate": 5, "comment": map[string]string{"en": "x"}, "company_name": "Acme"},
		})
		if resp.Status != http.StatusForbidden {
			t.Errorf("want 403 for tenant-admin token, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("public get all returns rate, comment, company name, company owner name", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/reviews?page=1&limit=50", testutil.ReqOpts{})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		var found map[string]interface{}
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if m["id"] == reviewID {
				found = m
			}
		}
		if found == nil {
			t.Fatal("expected created review in public list")
		}
		if rate, _ := found["rate"].(float64); rate != 5 {
			t.Errorf("want rate 5, got %v", found["rate"])
		}
		if found["comment"] != "Inno Nomads shipped our site faster than we expected." {
			t.Errorf("want resolved en comment, got %v", found["comment"])
		}
		if found["company_name"] != "Nomad Retail LLC" {
			t.Errorf("want company_name, got %v", found["company_name"])
		}
		if found["company_owner_name"] != "Bat-Erdene O." {
			t.Errorf("want company_owner_name, got %v", found["company_owner_name"])
		}
	})

	t.Run("public get all resolves locale", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/reviews?lang=mn&page=1&limit=50", testutil.ReqOpts{})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		var found map[string]interface{}
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if m["id"] == reviewID {
				found = m
			}
		}
		if found == nil {
			t.Fatal("expected created review in public list")
		}
		if found["comment"] != "Инно Номадс манай сайтыг хүлээснээс хурдан гаргаж өгсөн." {
			t.Errorf("want localized mn comment, got %v", found["comment"])
		}
	})

	t.Run("update rejects out-of-range rate", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/reviews/"+reviewID, testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]interface{}{"rate": 0},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("update succeeds", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/reviews/"+reviewID, testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]interface{}{"rate": 4},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("delete removes it from the public list", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodDelete, "/api/v1/platform/reviews/"+reviewID, testutil.ReqOpts{
			Token: superadmin,
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		listResp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/reviews?page=1&limit=50", testutil.ReqOpts{})
		items, _ := listResp.Body["data"].([]interface{})
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if m["id"] == reviewID {
				t.Error("expected deleted review to be excluded from the public list")
			}
		}
	})
}
