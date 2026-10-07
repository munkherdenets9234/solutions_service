package public

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// subscriptionStatusController tells a tenant's own front end whether its
// subscription has lapsed, so it can show a notice before a write is refused.
type subscriptionStatusController struct {
	svc *service.SubscriptionStatusService
}

// Get answers {"state": "active"|"expired"} and nothing else: no plan, period
// or status detail leaves this route. A nil service reads as active (the
// service itself is nil-safe), never a panic.
func (h *subscriptionStatusController) Get(c *gin.Context) error {
	state := h.svc.State(c.Request.Context(), apictx.TenantID(c))
	// The answer is per tenant, so shared caches must not store it.
	c.Header("Cache-Control", "private, max-age=60")
	response.OK(c, gin.H{"state": string(state)})
	return nil
}
