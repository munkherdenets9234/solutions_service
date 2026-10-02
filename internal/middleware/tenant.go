package middleware

import (
	"net/url"
	"strings"

	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/gin-gonic/gin"
)

type TenantMiddleware struct {
	svc *service.TenantService
}

func NewTenantMiddleware(svc *service.TenantService) *TenantMiddleware {
	return &TenantMiddleware{svc: svc}
}

// Require resolves the tenant from the X-API-Key header and stores its ID
// in the gin context under CtxTenantID for downstream handlers/repos to
// scope all reads and writes by.
func (t *TenantMiddleware) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			fail(c, apierr.Unauthorized("missing X-API-Key header").In(apierr.DomainTenant))
			return
		}

		tenant, err := t.svc.Resolve(c.Request.Context(), apiKey)
		if err != nil {
			// Resolve already returns taxonomy errors; anything else becomes
			// a 500 in ErrorHandler with the cause logged.
			fail(c, err)
			return
		}

		if tenant.Domain != "" && !requestMatchesDomain(c, tenant.Domain) {
			fail(c, apierr.Forbidden("API key is not authorized for this domain").In(apierr.DomainTenant))
			return
		}

		c.Set(CtxTenantID, tenant.ID)
		c.Next()
	}
}

// requestMatchesDomain checks the request's Origin (falling back to Referer)
// against the tenant's registered domain. Browser requests always carry one
// of these; server-to-server calls (SSR, mobile apps, Postman) typically
// carry neither, so the check is skipped when both are absent rather than
// failing closed — the domain restriction defends against a key leaked into
// client-side JS being reused from an unauthorized site, not against
// server-side misuse.
func requestMatchesDomain(c *gin.Context, domain string) bool {
	host := requestHost(c.GetHeader("Origin"))
	if host == "" {
		host = requestHost(c.GetHeader("Referer"))
	}
	if host == "" {
		return true
	}
	return strings.EqualFold(host, domain)
}

func requestHost(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
