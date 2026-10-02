package private

import (
	"net/http"

	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// translationBodyLimit caps a page save. The service bounds entry count and text
// length, but only after the whole body is decoded; this stops a huge body
// before that.
const translationBodyLimit = 512 << 10

// translationsController is the tenant admin's editor for the wording of the
// public site. Every route sits behind Auth("admin") and the subscription gate.
type translationsController struct {
	svc *service.SitePageService
}

func (h *translationsController) List(c *gin.Context) error {
	out, err := h.svc.List(c.Request.Context(), apictx.TenantID(c))
	if err != nil {
		return err
	}
	response.OK(c, out)
	return nil
}

func (h *translationsController) Get(c *gin.Context) error {
	p, err := h.svc.Get(c.Request.Context(), apictx.TenantID(c), c.Param("page"))
	if err != nil {
		return err
	}
	response.OK(c, p)
	return nil
}

// Save replaces one page. The acting user is always passed: the repository
// clears user_id when it is nil.
func (h *translationsController) Save(c *gin.Context) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, translationBodyLimit)
	var body struct {
		Entries []models.ContentEntry `json:"entries"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		if _, tooBig := err.(*http.MaxBytesError); tooBig {
			return apierr.New(http.StatusRequestEntityTooLarge, apierr.DomainGeneral, apierr.CodeLimitExceeded, "the request body is larger than 512 KB")
		}
		return apierr.BadRequest("invalid request body")
	}
	n, err := h.svc.Save(c.Request.Context(), apictx.TenantID(c), c.Param("page"), body.Entries, apictx.ActorID(c))
	if err != nil {
		return err
	}
	response.OK(c, gin.H{"saved": true, "entries": n})
	return nil
}
