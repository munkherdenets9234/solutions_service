package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/pkg/token"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// TestMailOutboxRoutesAreAdminOnly: the mail log shows who was emailed, so the
// storefront key alone must not reach it (401), and a staff token must be
// refused (403). Both come from the /admin group's Auth("admin").
func TestMailOutboxRoutesAreAdminOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	maker, err := token.NewMaker(strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	staff, _, err := maker.CreateToken(primitive.NewObjectID().Hex(), "staff", primitive.NewObjectID().Hex(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	e := NewServer(Deps{
		Log:            zap.NewNop(),
		Config:         &config.Config{AppEnv: config.EnvTest},
		Auth:           middleware.NewAuthMiddleware(maker),
		TenantMW:       middleware.NewTenantMiddleware(fakeResolver{}),
		SubscriptionMW: middleware.NewSubscriptionMiddleware(entitlement.Unenforced{}),
	}).Handler()

	const prefix = "/api/v1/admin/mail-outbox"
	checked := 0
	for _, r := range e.Routes() {
		if !strings.HasPrefix(r.Path, prefix) {
			continue
		}
		checked++
		for name, tok := range map[string]string{"no token": "", "staff token": staff} {
			want := http.StatusUnauthorized
			if tok != "" {
				want = http.StatusForbidden
			}
			req := httptest.NewRequest(r.Method, fillParams(r.Path), strings.NewReader(`{}`))
			req.Header.Set("X-API-Key", "test-key")
			req.Header.Set("Content-Type", "application/json")
			if tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			w := httptest.NewRecorder()
			e.ServeHTTP(w, req)
			if w.Code != want {
				t.Errorf("%s %s with %s: got %d, want %d", r.Method, r.Path, name, w.Code, want)
			}
		}
	}
	if checked != 2 {
		t.Fatalf("found %d mail-outbox routes, want 2", checked)
	}
}
