package middleware

import (
	"strings"

	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/token"
	"github.com/gin-gonic/gin"
)

// TenantcoreAuth guards routes meant for platform staff signed in to
// tenantcore. Its tokens are Ed25519, not this service's HMAC ones, so it is
// separate from AuthMiddleware.
type TenantcoreAuth struct {
	verifier *token.TenantcoreVerifier
}

func NewTenantcoreAuth(v *token.TenantcoreVerifier) *TenantcoreAuth {
	return &TenantcoreAuth{verifier: v}
}

// RequireSuperadmin accepts only a valid tenantcore superadmin token. With no
// verifier configured the group is switched off and answers 404, as if the
// routes did not exist.
func (a *TenantcoreAuth) RequireSuperadmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a == nil || a.verifier == nil {
			fail(c, apierr.NotFound("route"))
			return
		}

		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			fail(c, apierr.Unauthorized("missing authorization header"))
			return
		}

		claims, err := a.verifier.Verify(strings.TrimPrefix(authHeader, "Bearer "))
		if err != nil {
			fail(c, apierr.Unauthorized(err.Error()))
			return
		}
		if claims.Role != token.TenantcoreRoleSuperadmin {
			fail(c, apierr.Forbidden(""))
			return
		}

		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxRole, string(claims.Role))
		c.Next()
	}
}
