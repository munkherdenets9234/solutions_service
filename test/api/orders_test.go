package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/eandstravel/digitalservice/internal/testutil"
)

func createDestination(t *testing.T, app *testutil.App, tenant testutil.Tenant) string {
	t.Helper()
	resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/destinations", testutil.ReqOpts{
		Token: tenant.AdminToken, APIKey: tenant.APIKey,
		Body: map[string]interface{}{"name": "Gobi Classic", "slug": fmt.Sprintf("gobi-%d", testutil.Unique())},
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("setup: create destination failed: %d %s", resp.Status, resp.Raw)
	}
	id, _ := resp.Data()["id"].(string)
	return id
}

func createCar(t *testing.T, app *testutil.App, tenant testutil.Tenant) string {
	t.Helper()
	resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/cars", testutil.ReqOpts{
		Token: tenant.AdminToken, APIKey: tenant.APIKey,
		Body: map[string]interface{}{"name": "Land Cruiser", "slug": fmt.Sprintf("car-%d", testutil.Unique())},
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("setup: create car failed: %d %s", resp.Status, resp.Raw)
	}
	id, _ := resp.Data()["id"].(string)
	return id
}

func sampleCustomer() map[string]interface{} {
	return map[string]interface{}{
		"name":  "Sarah Connor",
		"email": fmt.Sprintf("sarah-%d@example.com", testutil.Unique()),
		"phone": "+1-555-0142",
	}
}

func TestBookings(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "bookingco")
	destID := createDestination(t, app, tenant)

	var bookingID string

	t.Run("invalid destination_id rejected", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/bookings", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body: map[string]interface{}{
				"destination_id": "not-an-object-id",
				"customer":       sampleCustomer(),
				"booking":        map[string]interface{}{"total_price_usd": 2480},
			},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("unknown destination_id is not found", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/bookings", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body: map[string]interface{}{
				"destination_id": dummyID,
				"customer":       sampleCustomer(),
				"booking":        map[string]interface{}{"total_price_usd": 2480},
			},
		})
		if resp.Status != http.StatusNotFound {
			t.Errorf("want 404, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create succeeds (public, no auth)", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/bookings", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body: map[string]interface{}{
				"destination_id": destID,
				"customer":       sampleCustomer(),
				"booking":        map[string]interface{}{"total_price_usd": 2480},
			},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		bookingID, _ = resp.Data()["id"].(string)
		if status, _ := resp.Data()["status"].(string); status != "pending" {
			t.Errorf("want initial status pending, got %q", status)
		}
	})

	t.Run("admin list/get requires admin auth", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, "/api/v1/admin/bookings/"+bookingID, testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("update status", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPut, "/api/v1/admin/bookings/"+bookingID+"/status", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]string{"status": "confirmed"},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", resp.Status, resp.Raw)
		}
	})
}

func TestRentals(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "rentalco")
	carID := createCar(t, app, tenant)

	var rentalID string

	t.Run("unknown car_id is not found", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/rentals", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body: map[string]interface{}{
				"car_id":   dummyID,
				"customer": sampleCustomer(),
				"rental":   map[string]interface{}{"mode": "self_drive"},
			},
		})
		if resp.Status != http.StatusNotFound {
			t.Errorf("want 404, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create succeeds (public, no auth)", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/rentals", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body: map[string]interface{}{
				"car_id":   carID,
				"customer": sampleCustomer(),
				"rental":   map[string]interface{}{"mode": "with_driver"},
			},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		rentalID, _ = resp.Data()["id"].(string)
		if confID, _ := resp.Data()["confirmation_id"].(string); confID == "" {
			t.Error("expected a confirmation_id to be assigned")
		}
	})

	t.Run("admin get and update status", func(t *testing.T) {
		getResp := testutil.Do(t, app, http.MethodGet, "/api/v1/admin/rentals/"+rentalID, testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
		})
		if getResp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", getResp.Status, getResp.Raw)
		}

		updResp := testutil.Do(t, app, http.MethodPut, "/api/v1/admin/rentals/"+rentalID+"/status", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]string{"status": "confirmed"},
		})
		if updResp.Status != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", updResp.Status, updResp.Raw)
		}
	})
}

func TestAirportTransfers(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "transferco")

	var transferID string

	t.Run("invalid tier rejected", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/airport-transfers", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body: map[string]interface{}{
				"customer": sampleCustomer(),
				"transfer": map[string]interface{}{"tier": "gold-class", "passengers": 2},
			},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create succeeds (public, no auth)", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/airport-transfers", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body: map[string]interface{}{
				"customer": sampleCustomer(),
				"transfer": map[string]interface{}{"tier": "premium", "passengers": 3, "flight_number": "OM207"},
			},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		transferID, _ = resp.Data()["id"].(string)
	})

	t.Run("admin list/get/update-status", func(t *testing.T) {
		listResp := testutil.Do(t, app, http.MethodGet, "/api/v1/admin/airport-transfers?page=1&limit=50", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
		})
		if listResp.Status != http.StatusOK {
			t.Fatalf("list: want 200, got %d: %s", listResp.Status, listResp.Raw)
		}

		updResp := testutil.Do(t, app, http.MethodPut, "/api/v1/admin/airport-transfers/"+transferID+"/status", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]string{"status": "confirmed"},
		})
		if updResp.Status != http.StatusOK {
			t.Fatalf("update: want 200, got %d: %s", updResp.Status, updResp.Raw)
		}
	})
}

func TestContactMessages(t *testing.T) {
	db := testutil.StartMongo(t)
	app := testutil.NewApp(t, db)
	superadmin := testutil.SuperadminToken(t, app)
	tenant := testutil.NewTenant(t, app, superadmin, "contactco")

	var messageID string

	t.Run("missing required fields rejected", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/contact", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body:   map[string]string{"subject": "Hi"},
		})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("want 400, got %d: %s", resp.Status, resp.Raw)
		}
	})

	t.Run("create succeeds (public, no auth)", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodPost, "/api/v1/contact", testutil.ReqOpts{
			APIKey: tenant.APIKey,
			Body: map[string]string{
				"name":    "Tomas Ibarra",
				"email":   fmt.Sprintf("tomas-%d@example.com", testutil.Unique()),
				"subject": "Group booking",
				"message": "Looking to book for 15 people.",
			},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", resp.Status, resp.Raw)
		}
		messageID, _ = resp.Data()["id"].(string)
		if status, _ := resp.Data()["status"].(string); status != "new" {
			t.Errorf("want initial status new, got %q", status)
		}
	})

	t.Run("admin list/update-status", func(t *testing.T) {
		listResp := testutil.Do(t, app, http.MethodGet, "/api/v1/admin/contact-messages?page=1&limit=50", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
		})
		if listResp.Status != http.StatusOK {
			t.Fatalf("list: want 200, got %d: %s", listResp.Status, listResp.Raw)
		}

		updResp := testutil.Do(t, app, http.MethodPut, "/api/v1/admin/contact-messages/"+messageID+"/status", testutil.ReqOpts{
			Token: tenant.AdminToken, APIKey: tenant.APIKey,
			Body: map[string]string{"status": "read"},
		})
		if updResp.Status != http.StatusOK {
			t.Fatalf("update: want 200, got %d: %s", updResp.Status, updResp.Raw)
		}
	})
}
