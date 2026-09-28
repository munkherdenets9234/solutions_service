package public

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/dto"
	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// catalogController serves the platform's own published catalog — packages,
// testimonials and the public quote intake. Read-only apart
// from CreateQuote, which is the one thing an anonymous prospect may write.
type catalogController struct {
	pkg           *service.PackageService
	tenantPackage *service.TenantPackageService
	tenantReview  *service.TenantReviewService
	quote         *service.QuoteService
}

// ListPackages returns every package in the platform's catalog, active and
// inactive, with full locale maps intact.
func (h *catalogController) ListPackages(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)

	data, total, err := h.pkg.ListAll(c.Request.Context(), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

// GetPackage returns a single catalog package with full locale maps intact,
// for the platform admin's edit form.
func (h *catalogController) GetPackage(c *gin.Context) error {
	p, err := h.pkg.GetByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		return err
	}
	response.OK(c, p)
	return nil
}

// ListTenantPackages is the platform's view of one tenant's package
// assignments — every assigned package regardless of active status.
func (h *catalogController) ListTenantPackages(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)

	data, total, err := h.tenantPackage.ListForTenant(c.Request.Context(), c.Param("id"), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

// ListReviews is the public testimonials listing — active tenant reviews
// only, resolved to a single locale.
func (h *catalogController) ListReviews(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)

	data, total, err := h.tenantReview.List(c.Request.Context(), page, limit)
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	response.List(c, dto.ToTenantReviewResponses(data, locale), response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

// CreateQuote is the "request a quote" submission with no existing tenant
// relationship — a prospect inquiring before ever signing up. Fully public:
// no X-API-Key, no bearer token, which is why it is rate limited.
func (h *catalogController) CreateQuote(c *gin.Context) error {
	var q models.Quote
	if err := c.ShouldBindJSON(&q); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.quote.Create(c.Request.Context(), nil, &q); err != nil {
		return err
	}
	response.Created(c, q)
	return nil
}

// ListQuotes is every lead across the platform, tenant-linked and tenant-less
// alike — the platform admin's consolidated view.
func (h *catalogController) ListQuotes(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)

	data, total, err := h.quote.ListAll(c.Request.Context(), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}
