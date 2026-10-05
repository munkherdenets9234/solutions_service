package private

import (
	"time"

	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// guideApplicationsController is the tenant admin's view of guide applications.
// Applicant data is personal, so it is mounted only behind the admin bearer.
type guideApplicationsController struct {
	svc *service.GuideApplicationService
}

func (h *guideApplicationsController) List(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	f := repository.GuideListFilter{
		Status:   models.GuideStatus(c.Query("status")),
		Q:        c.Query("q"),
		Language: c.Query("language"),
		Region:   c.Query("region"),
	}
	rows, total, err := h.svc.List(c.Request.Context(), apictx.TenantID(c), f, page, limit)
	if err != nil {
		return err
	}
	response.List(c, rows, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *guideApplicationsController) Counts(c *gin.Context) error {
	counts, err := h.svc.Counts(c.Request.Context(), apictx.TenantID(c))
	if err != nil {
		return err
	}
	out := make(map[string]int64, len(models.GuideStatuses))
	for _, st := range models.GuideStatuses {
		out[string(st)] = counts[st]
	}
	response.OK(c, out)
	return nil
}

func (h *guideApplicationsController) Get(c *gin.Context) error {
	a, err := h.svc.Get(c.Request.Context(), apictx.TenantID(c), c.Param("id"))
	if err != nil {
		return err
	}
	response.OK(c, a)
	return nil
}

func (h *guideApplicationsController) SetStatus(c *gin.Context) error {
	var body struct {
		Status models.GuideStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	if err := h.svc.SetStatus(c.Request.Context(), apictx.TenantID(c), c.Param("id"), body.Status, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *guideApplicationsController) AddNote(c *gin.Context) error {
	var body struct {
		Text string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest("invalid request body")
	}
	if err := h.svc.AddNote(c.Request.Context(), apictx.TenantID(c), c.Param("id"), body.Text, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

// FileLink answers with a short-lived signed URL. The URL is a credential: it
// is returned to the authenticated admin and never logged.
func (h *guideApplicationsController) FileLink(c *gin.Context) error {
	url, expires, err := h.svc.FileDownload(c.Request.Context(), apictx.TenantID(c), c.Param("id"), c.Param("fileId"))
	if err != nil {
		return err
	}
	response.OK(c, gin.H{"url": url, "expires_at": expires.UTC().Format(time.RFC3339)})
	return nil
}
