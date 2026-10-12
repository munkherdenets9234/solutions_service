package testutil

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/pkg/apikey"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// TenantCoreServiceKey is the service key the stub accepts. Generated per test
// binary run, like TokenSecret.
var TenantCoreServiceKey = randomHex(20)

// startTenantCoreStub serves the three Tenant Core endpoints the service calls
// at runtime, so the app can start and resolve tenants without a real Tenant
// Core:
//
//   - GET  /api/v1/svc/tenants/resolve        X-Tenant-Key -> tenant identity
//   - GET  /api/v1/svc/entitlements/{id}      404, which the service treats as
//     "no subscription record" (no module or limit gates)
//   - POST /api/v1/svc/notifications/email    accepted and dropped
//
// Tenants are read from the service's own tenants collection in db (the tests
// create them through /platform/tenants), matched by the SHA-256 of the key.
func startTenantCoreStub(t testing.TB, db *mongo.Database) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Service-Key") != TenantCoreServiceKey {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"success": false,
					"error":   map[string]any{"domain": "SERVICE"},
				})
				return
			}
			h(w, r)
		}
	}

	mux.HandleFunc("GET /api/v1/svc/tenants/resolve", authed(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Tenant-Key")
		var tn struct {
			ID     any    `bson:"_id"`
			Name   string `bson:"name"`
			Slug   string `bson:"slug"`
			Domain string `bson:"domain"`
			Status string `bson:"status"`
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		err := db.Collection("tenants").
			FindOne(ctx, bson.M{"api_key_hash": apikey.Hash(key)}).Decode(&tn)
		if key == "" || err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"success": false,
				"error":   map[string]any{"domain": "TENANT"},
			})
			return
		}
		data := map[string]any{
			"tenant_id": tn.ID,
			"slug":      tn.Slug,
			"name":      tn.Name,
			"status":    tn.Status,
			"domain":    tn.Domain,
			"hosts":     []string{},
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": data})
	}))

	mux.HandleFunc("GET /api/v1/svc/entitlements/", authed(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false})
	}))

	mux.HandleFunc("POST /api/v1/svc/notifications/email", authed(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	}))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Logf("tenantcore stub: unhandled %s %s", r.Method, strings.TrimSpace(r.URL.Path))
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
