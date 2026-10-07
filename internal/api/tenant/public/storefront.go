package public

import (
	"strconv"

	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/dto"
	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// storefrontController serves what a visitor to a tenant's own site sees.
//
// Every response goes through internal/dto, which resolves the stored locale
// maps down to the one locale the request asked for. That projection is not
// cosmetic: the admin reads above deliberately return the full maps, and
// keeping the two shapes in separate call paths is what stops a field meant
// for the CMS from turning up in a storefront response because one shared
// type got widened.
type storefrontController struct {
	destination *service.DestinationService
	blog        *service.BlogService
	car         *service.CarService
	review      *service.ReviewService
	partner     *service.PartnerService
	pkg         *service.PackageService
}

func (h *storefrontController) ListDestinations(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)

	var featured *bool
	if raw := c.Query("featured"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return apierr.BadRequest("invalid featured value")
		}
		featured = &b
	}

	data, total, err := h.destination.List(c.Request.Context(), apictx.TenantID(c), service.ListDestinationsFilter{
		Category: c.Query("category"),
		Region:   c.Query("region"),
		Season:   c.Query("season"),
		Featured: featured,
		Page:     page,
		Limit:    limit,
	})
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	response.List(c, dto.ToDestinationResponses(data, locale), response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *storefrontController) GetDestination(c *gin.Context) error {
	d, err := h.destination.GetBySlug(c.Request.Context(), apictx.TenantID(c), c.Param("slug"))
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	response.OK(c, dto.ToDestinationResponse(d, locale))
	return nil
}

func (h *storefrontController) ListBlogs(c *gin.Context) error {
	page, limit := apictx.Page(c, 10)

	data, total, err := h.blog.ListPublished(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	response.List(c, dto.ToBlogResponses(data, locale), response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *storefrontController) GetBlog(c *gin.Context) error {
	b, err := h.blog.GetBySlug(c.Request.Context(), apictx.TenantID(c), c.Param("slug"))
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	response.OK(c, dto.ToBlogResponse(b, locale))
	return nil
}

func (h *storefrontController) ListCars(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)

	data, total, err := h.car.List(c.Request.Context(), apictx.TenantID(c), service.ListCarsFilter{
		Type:  c.Query("type"),
		Fuel:  c.Query("fuel"),
		Page:  page,
		Limit: limit,
	})
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *storefrontController) GetCar(c *gin.Context) error {
	car, err := h.car.GetBySlug(c.Request.Context(), apictx.TenantID(c), c.Param("slug"))
	if err != nil {
		return err
	}
	response.OK(c, car)
	return nil
}

func (h *storefrontController) ListPartners(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)

	data, total, err := h.partner.List(c.Request.Context(), apictx.TenantID(c), service.ListPartnersFilter{
		Tag:   c.Query("tag"),
		Page:  page,
		Limit: limit,
	})
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	response.List(c, dto.ToPartnerResponses(data, locale), response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *storefrontController) GetPartner(c *gin.Context) error {
	p, err := h.partner.GetBySlug(c.Request.Context(), apictx.TenantID(c), c.Param("slug"))
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	response.OK(c, dto.ToPartnerResponse(p, locale))
	return nil
}

// ListPackages is the tenant-scoped storefront price list — the catalog
// packages assigned to this tenant, not the platform's whole catalog.
func (h *storefrontController) ListPackages(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)

	data, total, err := h.pkg.ListForTenant(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	response.List(c, dto.ToPackageResponses(data, locale), response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *storefrontController) GetPackage(c *gin.Context) error {
	p, err := h.pkg.GetBySlugForTenant(c.Request.Context(), apictx.TenantID(c), c.Param("slug"))
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	response.OK(c, dto.ToPackageResponse(p, locale))
	return nil
}

func (h *storefrontController) ListReviews(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)

	data, total, err := h.review.List(c.Request.Context(), apictx.TenantID(c), service.ListReviewsFilter{
		Tour:    c.Query("tour"),
		Partner: c.Query("partner"),
		Page:    page,
		Limit:   limit,
	})
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	avatars, err := h.review.AvatarsFor(c.Request.Context(), apictx.TenantID(c), data)
	if err != nil {
		return err
	}
	response.List(c, dto.ToReviewResponsesWithAvatars(data, locale, avatars), response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

// CreateReview is a site visitor submitting a single-locale review, with no
// auth beyond the tenant's X-API-Key — same as the other lead forms. There is
// no actor to record, so the audit link is nil rather than invented.
func (h *storefrontController) CreateReview(c *gin.Context) error {
	var req dto.CreatePublicReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return apierr.BadRequest(err.Error())
	}
	rev := req.ToModel(i18n.ResolveFromRequest(c))
	if err := h.review.Create(c.Request.Context(), apictx.TenantID(c), rev, nil); err != nil {
		return err
	}
	response.Created(c, rev)
	return nil
}
