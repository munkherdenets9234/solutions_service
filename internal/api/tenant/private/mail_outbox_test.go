package private

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/httpx"
	"github.com/eandstravel/digitalservice/pkg/token"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

type fakeMailLog struct {
	listCalls, retryCalls int
}

func (f *fakeMailLog) List(context.Context, primitive.ObjectID, string, int, int) ([]*service.MailLogItem, int64, error) {
	f.listCalls++
	return nil, 0, nil
}

func (f *fakeMailLog) Retry(context.Context, primitive.ObjectID, string) error {
	f.retryCalls++
	return nil
}

// TestRetryRequiresAdminRole mounts the controller through the same function
// Register uses, on a group behind the real Auth middleware with Auth("admin"),
// and mints real tokens: a staff token gets 403 on both endpoints, no token
// gets 401, an admin token reaches the handler.
func TestRetryRequiresAdminRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	maker, err := token.NewMaker(strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	tenant := primitive.NewObjectID()
	mint := func(role string) string {
		tok, _, err := maker.CreateToken(primitive.NewObjectID().Hex(), role, tenant.Hex(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}

	f := &fakeMailLog{}
	e := gin.New()
	e.Use(middleware.ErrorHandler(zap.NewNop(), false))
	e.Use(func(c *gin.Context) { c.Set(middleware.CtxTenantID, tenant) })
	auth := middleware.NewAuthMiddleware(maker).Require
	admin := httpx.Wrap(e.Group("/admin", auth("admin")))
	registerMailOutbox(admin, &mailOutboxController{svc: f})

	do := func(method, path, tok string) int {
		req := httptest.NewRequest(method, path, nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}
	id := primitive.NewObjectID().Hex()
	for _, ep := range []struct{ method, path string }{
		{http.MethodGet, "/admin/mail-outbox"},
		{http.MethodPost, "/admin/mail-outbox/" + id + "/retry"},
	} {
		if got := do(ep.method, ep.path, mint("staff")); got != http.StatusForbidden {
			t.Errorf("%s %s staff: got %d, want 403", ep.method, ep.path, got)
		}
		if got := do(ep.method, ep.path, ""); got != http.StatusUnauthorized {
			t.Errorf("%s %s no token: got %d, want 401", ep.method, ep.path, got)
		}
		if got := do(ep.method, ep.path, mint("admin")); got != http.StatusOK {
			t.Errorf("%s %s admin: got %d, want 200", ep.method, ep.path, got)
		}
	}
	if f.listCalls != 1 || f.retryCalls != 1 {
		t.Errorf("handler calls: list %d retry %d, want 1 each (staff and anonymous must not reach it)", f.listCalls, f.retryCalls)
	}
}
