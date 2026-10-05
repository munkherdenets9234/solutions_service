package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/internal/tenantresolve"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TenantRef is what the middleware needs to know about a resolved tenant.
type TenantRef struct {
	ID     primitive.ObjectID
	Domain string
}

// TenantResolver turns a raw X-API-Key into a tenant. Its errors are already
// in the apierr taxonomy (401 unknown key, 403 suspended, 503 could not find
// out); the middleware passes them on unchanged.
type TenantResolver interface {
	Resolve(ctx context.Context, rawKey string) (TenantRef, error)
}

// tenantLookup is the one method of *service.TenantService the local resolver
// uses. It exists so the adapter can be tested without a database.
type tenantLookup interface {
	Resolve(ctx context.Context, rawAPIKey string) (*models.Tenant, error)
}

type localResolver struct {
	lookup tenantLookup
}

// NewLocalResolver resolves against this service's own tenants collection, as
// it always has.
func NewLocalResolver(svc *service.TenantService) TenantResolver {
	return localResolver{lookup: svc}
}

func (l localResolver) Resolve(ctx context.Context, rawKey string) (TenantRef, error) {
	t, err := l.lookup.Resolve(ctx, rawKey)
	if err != nil {
		return TenantRef{}, err
	}
	return TenantRef{ID: t.ID, Domain: t.Domain}, nil
}

type tenantcoreResolver struct {
	client *tenantresolve.Client
}

// NewTenantcoreResolver resolves through tenantcore, which owns tenants.
func NewTenantcoreResolver(c *tenantresolve.Client) TenantResolver {
	return tenantcoreResolver{client: c}
}

func (r tenantcoreResolver) Resolve(ctx context.Context, rawKey string) (TenantRef, error) {
	id, err := r.client.Resolve(ctx, rawKey)
	if err != nil {
		if errors.Is(err, tenantresolve.ErrUnknownKey) {
			// Same body as the local resolver's unknown-key answer.
			return TenantRef{}, apierr.Unauthorized("")
		}
		// ErrUnavailable or anything unexpected: tenantcore could not be
		// asked, so there is no honest answer. 503, never 401 — telling a
		// valid storefront its key is wrong because the platform is down
		// would be the worst available answer.
		return TenantRef{}, apierr.Wrap(err, http.StatusServiceUnavailable, apierr.DomainTenant,
			apierr.CodeFeatureUnavailable, "tenant lookup is temporarily unavailable")
	}
	if id.Suspended {
		return TenantRef{}, apierr.Forbidden("tenant suspended").In(apierr.DomainTenant)
	}
	return TenantRef{ID: id.TenantID, Domain: id.Domain}, nil
}

type TenantMiddleware struct {
	resolver TenantResolver
}

func NewTenantMiddleware(r TenantResolver) *TenantMiddleware {
	return &TenantMiddleware{resolver: r}
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

		tenant, err := t.resolver.Resolve(c.Request.Context(), apiKey)
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
