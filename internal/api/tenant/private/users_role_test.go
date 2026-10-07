package private

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/pkg/token"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// TestUpdateUserRefusesStaffAndAnonymous runs the real Register behind the real
// Auth middleware: PUT /admin/users/:id (which now sets receive_emails) must
// answer 403 to a staff token and 401 to no token, before any controller or
// service runs (the services are nil here, so reaching one would panic).
func TestUpdateUserRefusesStaffAndAnonymous(t *testing.T) {
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
	pass := func(c *gin.Context) { c.Next() }
	e := gin.New()
	e.Use(middleware.ErrorHandler(zap.NewNop(), false))
	e.Use(func(c *gin.Context) { c.Set(middleware.CtxTenantID, tenant) })
	Register(e.Group(""), Deps{
		Auth:          middleware.NewAuthMiddleware(maker).Require,
		Subscription:  pass,
		AuthRateLimit: pass,
	})

	path := "/admin/users/" + primitive.NewObjectID().Hex()
	do := func(tok string) int {
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"receive_emails":false}`))
		req.Header.Set("Content-Type", "application/json")
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w.Code
	}
	if got := do(mint("staff")); got != http.StatusForbidden {
		t.Errorf("staff token: got %d, want 403", got)
	}
	if got := do(""); got != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", got)
	}
}
