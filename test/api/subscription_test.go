package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/eandstravel/digitalservice/internal/testutil"
)

func createPackage(t *testing.T, app *testutil.App, superadminToken string) string {
	t.Helper()
	resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/packages", testutil.ReqOpts{
		Token: superadminToken,
		Body: map[string]interface{}{
			"slug": fmt.Sprintf("pkg-%d", testutil.Unique()),
			"name": map[string]string{"en": "Test Package"},
		},
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("setup: create package failed: %d %s", resp.Status, resp.Raw)
	}
	id, _ := resp.Data()["id"].(string)
	return id
}

func TestTenantSubscription(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "subtenant")
	pkgA := createPackage(t, app, superadmin)
	pkgB := createPackage(t, app, superadmin)

	t.Run("get before create is not found", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+tenant.ID+"/subscription", testutil.ReqOpts{Token: superadmin})
		if resp.Status != http.StatusNotFound {
			t.Errorf("want 404, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create rejects unknown package_id", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants/"+tenant.ID+"/subscription", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"package_id": dummyID},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create with a package", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants/"+tenant.ID+"/subscription", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"package_id": pkgA},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		if pkgID, _ := resp.Data()["package_id"].(string); pkgID != pkgA {
			t.Errorf("want package_id %q, got %q", pkgA, pkgID)
		}
		if status, _ := resp.Data()["status"].(string); status != "active" {
			t.Errorf("want status active, got %q", status)
		}
		pkg, _ := resp.Data()["package"].(map[string]interface{})
		if pkg == nil || pkg["id"] != pkgA {
			t.Errorf("want resolved package in response, got %v", pkg)
		}
	})

	t.Run("create again conflicts", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/platform/tenants/"+tenant.ID+"/subscription", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"package_id": pkgB},
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
		if pkgID, _ := resp.Data()["package_id"].(string); pkgID != pkgA {
			t.Errorf("want package_id %q, got %q", pkgA, pkgID)
		}
	})

	t.Run("update package rejects invalid value", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+tenant.ID+"/subscription/package", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"package_id": dummyID},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("update package to a valid value", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+tenant.ID+"/subscription/package", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"package_id": pkgB},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}

		getResp := testutil.Do(t, app, http.MethodGet, "/api/v1/platform/tenants/"+tenant.ID+"/subscription", testutil.ReqOpts{Token: superadmin})
		if pkgID, _ := getResp.Data()["package_id"].(string); pkgID != pkgB {
			t.Errorf("want package_id %q after update, got %q", pkgB, pkgID)
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
		otherPkg := createPackage(t, app, superadmin)

		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/platform/tenants/"+otherTenant.ID+"/subscription/package", testutil.ReqOpts{
			Token: superadmin,
			Body:  map[string]string{"package_id": otherPkg},
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
