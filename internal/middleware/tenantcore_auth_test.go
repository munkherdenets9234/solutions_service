package middleware

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/pkg/token"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

func tcEngine(t *testing.T, v *token.TenantcoreVerifier) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(ErrorHandler(zap.NewNop(), false))
	e.GET("/x", NewTenantcoreAuth(v).RequireSuperadmin(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"role": c.GetString(CtxRole), "user": c.GetString(CtxUserID)})
	})
	return e
}

func tcSetup(t *testing.T) (ed25519.PrivateKey, *token.TenantcoreVerifier) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	v, err := token.NewVerifier(base64.StdEncoding.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	return priv, v
}

func tcToken(t *testing.T, priv ed25519.PrivateKey, role, tenant string) string {
	t.Helper()
	claims := token.TenantcoreClaims{
		UserID: "u1", Role: token.TenantcoreRole(role), TenantID: tenant,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    token.TenantcoreIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func tcGet(e *gin.Engine, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func TestRequireSuperadmin_NilVerifierIs404(t *testing.T) {
	if w := tcGet(tcEngine(t, nil), "anything"); w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}

func TestRequireSuperadmin_MissingBearerIs401(t *testing.T) {
	_, v := tcSetup(t)
	if w := tcGet(tcEngine(t, v), ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", w.Code)
	}
}

func TestRequireSuperadmin_InvalidTokenIs401(t *testing.T) {
	_, v := tcSetup(t)
	if w := tcGet(tcEngine(t, v), "garbage"); w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", w.Code)
	}
}

func TestRequireSuperadmin_TenantRoleIs403(t *testing.T) {
	priv, v := tcSetup(t)
	if w := tcGet(tcEngine(t, v), tcToken(t, priv, "admin", "t1")); w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

func TestRequireSuperadmin_SuperadminPasses(t *testing.T) {
	priv, v := tcSetup(t)
	w := tcGet(tcEngine(t, v), tcToken(t, priv, "superadmin", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
}
