package api

import (
	"net/http"
	"testing"

	"github.com/eandstravel/digitalservice/internal/testutil"
)

func TestTenantSubscription(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "subtenant")

	t.Run("get before create is not found", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+tenant.ID+"/subscription", testutil.ReqOpts{Token: superadmin})
		if resp.Status != http.StatusNotFound {
			t.Errorf("want 404, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create with default plan", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants/"+tenant.ID+"/subscription", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		if plan, _ := resp.Data()["plan"].(string); plan != "free" {
			t.Errorf("want default plan free, got %q", plan)
		}
		if status, _ := resp.Data()["status"].(string); status != "active" {
			t.Errorf("want status active, got %q", status)
		}
	})

	t.Run("create again conflicts", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants/"+tenant.ID+"/subscription", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"plan": "pro"},
		})
		if resp.Status != http.StatusConflict {
			t.Errorf("want 409, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("get returns the created subscription", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+tenant.ID+"/subscription", testutil.ReqOpts{Token: superadmin})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
		if plan, _ := resp.Data()["plan"].(string); plan != "free" {
			t.Errorf("want plan free, got %q", plan)
		}
	})

	t.Run("update plan rejects invalid value", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+tenant.ID+"/subscription/plan", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"plan": "ultimate-mega-plan"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("update plan to a valid value", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+tenant.ID+"/subscription/plan", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"plan": "enterprise"},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		getResp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+tenant.ID+"/subscription", testutil.ReqOpts{Token: superadmin})
		if plan, _ := getResp.Data()["plan"].(string); plan != "enterprise" {
			t.Errorf("want plan enterprise after update, got %q", plan)
		}
	})

	t.Run("cancel marks it canceled", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants/"+tenant.ID+"/subscription/cancel", testutil.ReqOpts{Token: superadmin})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		getResp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+tenant.ID+"/subscription", testutil.ReqOpts{Token: superadmin})
		if status, _ := getResp.Data()["status"].(string); status != "canceled" {
			t.Errorf("want status canceled, got %q", status)
		}
		if getResp.Data()["canceled_at"] == nil {
			t.Error("expected canceled_at to be set")
		}
	})

	t.Run("update/cancel for a tenant with no subscription is not found", func(t *testing.T) {
		otherTenant := testutil.NewTenant(t, app, superadmin, "nosub")

		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+otherTenant.ID+"/subscription/plan", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"plan": "pro"},
		})
		if resp.Status != http.StatusNotFound {
			t.Errorf("update: want 404, got %d: %s", resp.Status, resp.Raw)
		}

		resp = testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants/"+otherTenant.ID+"/subscription/cancel", testutil.ReqOpts{Token: superadmin})
		if resp.Status != http.StatusNotFound {
			t.Errorf("cancel: want 404, got %d: %s", resp.Status, resp.Raw)
		}
	})
}
