package public

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/dto"
	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

type tenantsController struct {
	svc *service.TenantService
}

// ListProjects is the public "Our Projects" listing — active tenants with a
// showcase-enabled TenantDetail, locale-resolved.
func (h *tenantsController) ListProjects(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)

	tenants, details, total, err := h.svc.ListProjects(c.Request.Context(), page, limit)
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	response.List(c, dto.ToProjectResponses(tenants, details, locale), response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

// GetProjectBySlug is ListProjects' single-project counterpart, for the
// case-study detail page.
func (h *tenantsController) GetProjectBySlug(c *gin.Context) error {
	t, d, err := h.svc.GetProjectBySlug(c.Request.Context(), c.Param("slug"))
	if err != nil {
		return err
	}
	locale := i18n.ResolveFromRequest(c)
	response.OK(c, dto.ToProjectResponse(t, d, locale))
	return nil
}
