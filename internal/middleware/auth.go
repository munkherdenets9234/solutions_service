package middleware

import (
	"net/http"
	"strings"

	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/eandstravel/digitalservice/pkg/token"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
			response.Error(c, http.StatusUnauthorized, "missing authorization header")
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := a.maker.VerifyToken(tokenStr)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, err.Error())
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
				response.Error(c, http.StatusForbidden, "forbidden")
				return
			}
		}

		if claims.Role == "superadmin" {
			if claims.TenantID != "" {
				response.Error(c, http.StatusForbidden, "forbidden")
				return
			}
		} else if claims.TenantID != "" {
			if resolved, exists := c.Get("tenant_id"); exists {
				if claims.TenantID != resolved.(primitive.ObjectID).Hex() {
					response.Error(c, http.StatusForbidden, "token does not belong to this tenant")
					return
				}
			}
		}

		c.Set("user_id", claims.UserID)
		c.Set("role", claims.Role)
		c.Next()
	}
}
