package public

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// translationsController serves the tenant's wording overrides to its storefront.
type translationsController struct {
	svc *service.SitePageService
}

// Get returns {page: {path: value}} for ?lang=en|mn|ko. The service rejects any
// other lang, including a missing one, with a 400.
func (h *translationsController) Get(c *gin.Context) error {
	out, err := h.svc.Public(c.Request.Context(), apictx.TenantID(c), c.Query("lang"))
	if err != nil {
		return err
	}
	response.OK(c, out)
	return nil
}
