package middleware

import (
	"net/http"

	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

type TenantMiddleware struct {
	svc *service.TenantService
}

func NewTenantMiddleware(svc *service.TenantService) *TenantMiddleware {
	return &TenantMiddleware{svc: svc}
}

// Require resolves the tenant from the X-API-Key header and stores its ID
// in the gin context under "tenant_id" for downstream handlers/repos to
// scope all reads and writes by.
func (t *TenantMiddleware) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			response.Error(c, http.StatusUnauthorized, "missing X-API-Key header")
			return
		}

		tenant, err := t.svc.Resolve(c.Request.Context(), apiKey)
		if err != nil {
			if e, ok := err.(*apierr.APIError); ok {
				response.Error(c, e.StatusCode, e.Message)
				return
			}
			response.Error(c, http.StatusInternalServerError, "internal server error")
			return
		}

		c.Set("tenant_id", tenant.ID)
		c.Next()
	}
}
