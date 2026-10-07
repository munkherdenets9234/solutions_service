package private

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/httpx"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// mailLogService is the service surface this controller uses; a local
// interface so handler tests can fake it.
type mailLogService interface {
	List(ctx context.Context, tenantID primitive.ObjectID, status string, page, limit int) ([]*service.MailLogItem, int64, error)
	Retry(ctx context.Context, tenantID primitive.ObjectID, id string) error
}

// mailOutboxController is the tenant admin's view of staff notification mail.
type mailOutboxController struct {
	svc mailLogService
}

// registerMailOutbox mounts the mail log on the admin group. That group is
// Auth("admin") only, so a staff token is refused before the handler runs.
func registerMailOutbox(admin *httpx.G, h *mailOutboxController) {
	g := admin.Group("/mail-outbox")
	g.GET("", h.List)
	g.POST("/:id/retry", h.Retry)
}

func (h *mailOutboxController) List(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	page, limit = service.ClampPage(page, limit)
	items, total, err := h.svc.List(c.Request.Context(), apictx.TenantID(c), c.Query("status"), page, limit)
	if err != nil {
		return err
	}
	response.List(c, items, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *mailOutboxController) Retry(c *gin.Context) error {
	if err := h.svc.Retry(c.Request.Context(), apictx.TenantID(c), c.Param("id")); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}
