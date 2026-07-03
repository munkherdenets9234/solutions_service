package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/testutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestDestinations(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "destco")
	slug := fmt.Sprintf("gobi-classic-%d", testutil.Unique())

	var destID string

	t.Run("create requires slug", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/destinations", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]string{"name": "No Slug"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create succeeds", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/destinations", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]interface{}{"name": "Gobi Classic", "slug": slug, "region": "gobi"},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		destID, _ = resp.Data()["id"].(string)
		if destID == "" {
			t.Fatal("expected created destination id")
		}
		if active, _ := resp.Data()["is_active"].(bool); !active {
			t.Error("expected new destination to be active")
		}
	})

	t.Run("public get by slug", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/destinations/"+slug, testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("public list includes it", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/destinations?page=1&limit=50", testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		found := false
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == destID {
				found = true
			}
		}
		if !found {
			t.Error("expected created destination in public list")
		}
	})

	t.Run("featured filter", func(t *testing.T) {
		featuredSlug := fmt.Sprintf("featured-dest-%d", testutil.Unique())
		createResp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/destinations", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]interface{}{"name": "Featured Dest", "slug": featuredSlug, "featured": true},
		})
		if createResp.Status != http.StatusCreated {
			t.Fatalf("setup: want 201, got %d: %s", createResp.Status, createResp.Raw)
		}
		featuredID, _ := createResp.Data()["id"].(string)

		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/destinations?featured=true&page=1&limit=50", testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		var sawFeatured, sawNonFeatured bool
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			id, _ := m["id"].(string)
			if id == featuredID {
				sawFeatured = true
			}
			if id == destID {
				sawNonFeatured = true
			}
		}
		if !sawFeatured {
			t.Error("expected featured=true to include the featured destination")
		}
		if sawNonFeatured {
			t.Error("expected featured=true to exclude the non-featured destination")
		}

		invalidResp := testutil.Do(t, app, http.MethodGet, "/api/v1/destinations?featured=not-a-bool", testutil.ReqOpts{APIKey: tenant.APIKey})
		if invalidResp.Status != http.StatusBadRequest {
			t.Errorf("want 400 for invalid featured value, got %d: %s", invalidResp.Status, invalidResp.Raw)
		}
	})

	t.Run("not visible from another tenant", func(t *testing.T) {
		otherTenant := testutil.NewTenant(t, app, superadmin, "otherdestco")
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/destinations/"+slug, testutil.ReqOpts{APIKey: otherTenant.APIKey})
		if resp.Status != http.StatusNotFound {
			t.Errorf("want 404 (tenant isolation), got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("update (partial)", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/admin/destinations/"+destID, testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]interface{}{"overview": "Updated overview"},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("update unknown id is not found", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/admin/destinations/"+dummyID, testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]interface{}{"overview": "x"},
		})
		if resp.Status != http.StatusNotFound {
			t.Errorf("want 404, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("delete", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodDelete, "/api/v1/admin/destinations/"+destID, testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		getResp := testutil.Do(t, app, http.MethodGet, "/api/v1/destinations/"+slug, testutil.ReqOpts{APIKey: tenant.APIKey})
		if getResp.Status != http.StatusNotFound {
			t.Errorf("want 404 after delete, got %d: %s", getResp.Status, getResp.Raw)
		}
	})
}

func TestBlogs(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "blogco")
	slug := fmt.Sprintf("best-time-%d", testutil.Unique())

	var blogID string

	t.Run("create requires slug", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/blogs", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]string{"title": "No Slug"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create defaults to draft", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/blogs", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]string{"title": "Best Time to Visit", "slug": slug, "content": "<p>...</p>"},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		blogID, _ = resp.Data()["id"].(string)
		if status, _ := resp.Data()["status"].(string); status != "draft" {
			t.Errorf("want draft status, got %q", status)
		}
	})

	t.Run("draft is not in the public published list", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/blogs/"+slug, testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusNotFound {
			t.Errorf("want 404 for unpublished blog, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("publish", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/blogs/"+blogID+"/publish", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		getResp := testutil.Do(t, app, http.MethodGet, "/api/v1/blogs/"+slug, testutil.ReqOpts{APIKey: tenant.APIKey})
		if getResp.Status != http.StatusOK {
			t.Fatalf("want 200 after publish, got %d: %s", getResp.Status, getResp.Raw)
		}
	})

	t.Run("public list of published blogs includes it", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/blogs?page=1&limit=50", testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		items, _ := resp.Body["data"].([]interface{})
		found := false
		for _, it := range items {
			m, _ := it.(map[string]interface{})
			if id, _ := m["id"].(string); id == blogID {
				found = true
			}
		}
		if !found {
			t.Error("expected published blog in public list")
		}
	})

	t.Run("update (partial)", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/admin/blogs/"+blogID, testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]interface{}{"title": "Best Time to Visit (Updated)"},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})
}

func TestCars(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "carco")
	slug := fmt.Sprintf("land-cruiser-%d", testutil.Unique())

	var carID string

	t.Run("create requires slug", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/cars", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]string{"name": "No Slug"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create succeeds", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/cars", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]interface{}{"slug": slug, "name": "Land Cruiser", "type": "4x4", "fuel": "diesel", "price_per_day_usd": 95},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		carID, _ = resp.Data()["id"].(string)
	})

	t.Run("public list", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/cars?page=1&limit=20", testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("public list with type and fuel filter", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/cars?type=4x4&fuel=diesel&page=1&limit=20", testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("public list with no results", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/cars?type=nonexistent&page=1&limit=20", testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("public list second page", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/cars?page=2&limit=20", testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("public list with a doc missing optional fields", func(t *testing.T) {
		tid, _ := primitive.ObjectIDFromHex(tenant.ID)
		_, err := db.Collection("cars").InsertOne(context.Background(), bson.M{
			"tenant_id":   tid,
			"slug":        fmt.Sprintf("legacy-%d", testutil.Unique()),
			"name":        "Legacy Car",
			"is_active":   true,
			"cover_image": nil,
			"created_at":  time.Now(),
			"updated_at":  time.Now(),
		})
		if err != nil {
			t.Fatalf("setup insert failed: %v", err)
		}
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/cars?page=1&limit=20", testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("public get by slug", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/cars/"+slug, testutil.ReqOpts{APIKey: tenant.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("update price", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/admin/cars/"+carID, testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]interface{}{"price_per_day_usd": 85},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("delete", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodDelete, "/api/v1/admin/cars/"+carID, testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})
}
