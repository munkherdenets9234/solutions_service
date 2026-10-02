package middleware

import (
	"strings"

	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/token"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Context keys set by the auth and tenant middleware. Handlers read them
// through internal/api/apictx rather than by string literal, so a typo is a
// compile error rather than a nil map lookup at request time.
const (
	CtxUserID   = "user_id"
	CtxRole     = "role"
	CtxTenantID = "tenant_id"
)

type AuthMiddleware struct {
	maker *token.Maker
}

func NewAuthMiddleware(maker *token.Maker) *AuthMiddleware {
	return &AuthMiddleware{maker: maker}
}

// Require verifies the bearer token and, if roles are given, checks the
// token's role is one of them. A "superadmin" claim must never carry a
// tenant scope - that combination would let a tenant-scoped role that got
// smuggled into a token (e.g. via a bad role value) pass as a superadmin on
// platform routes, which aren't behind TenantMiddleware. If TenantMiddleware
// ran earlier in the chain and resolved a tenant from X-API-Key, and this
// token is tenant-scoped (non-superadmin), the token's tenant must match the
// resolved tenant - this stops an admin token issued for tenant A from being
// replayed against tenant B's API key.
func (a *AuthMiddleware) Require(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			fail(c, apierr.Unauthorized("missing authorization header"))
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := a.maker.VerifyToken(tokenStr)
		if err != nil {
			// The verifier's own text ("token has expired" vs "invalid
			// token") is useful to a legitimate client and tells an attacker
			// nothing they could not learn by waiting.
			fail(c, apierr.Unauthorized(err.Error()))
			return
		}

		if len(roles) > 0 {
			allowed := false
			for _, r := range roles {
				if claims.Role == r {
					allowed = true
					break
				}
			}
			if !allowed {
				fail(c, apierr.Forbidden(""))
				return
			}
		}

		if claims.Role == "superadmin" {
			if claims.TenantID != "" {
				fail(c, apierr.Forbidden(""))
				return
			}
		} else if claims.TenantID != "" {
			if resolved, exists := c.Get(CtxTenantID); exists {
				if claims.TenantID != resolved.(primitive.ObjectID).Hex() {
					fail(c, apierr.Forbidden("token does not belong to this tenant"))
					return
				}
			}
		}

		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxRole, claims.Role)
		c.Next()
	}
}
