package middleware

import (
	"net/http"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type SubscriptionMiddleware struct {
	svc *service.SubscriptionService
}

func NewSubscriptionMiddleware(svc *service.SubscriptionService) *SubscriptionMiddleware {
	return &SubscriptionMiddleware{svc: svc}
}

// Require blocks mutating requests (POST/PUT/DELETE/PATCH) for tenants whose
// subscription is expired or in a non-active state. GET/HEAD/OPTIONS always
// pass through so a tenant with a lapsed subscription can still view their
// data. A tenant with no subscription record at all is not held to any
// subscription state here — provisioning one is a separate, explicit step
// (see SubscriptionHandler) done by a platform superadmin. Must run after
// TenantMiddleware.Require(), which sets "tenant_id" in the context.
func (s *SubscriptionMiddleware) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			c.Next()
			return
		}

		tenantIDVal, exists := c.Get("tenant_id")
		if !exists {
			response.Error(c, http.StatusInternalServerError, "internal server error")
			return
		}
		tenantID := tenantIDVal.(primitive.ObjectID)

		sub, err := s.svc.Get(c.Request.Context(), tenantID)
		if err != nil {
			if e, ok := err.(*apierr.APIError); ok && e.StatusCode == http.StatusNotFound {
				c.Next()
				return
			}
			response.Error(c, http.StatusInternalServerError, "internal server error")
			return
		}

		if !isSubscriptionValid(sub) {
			response.Error(c, http.StatusPaymentRequired, "subscription is inactive or expired")
			return
		}

		c.Next()
	}
}

func isSubscriptionValid(sub *models.Subscription) bool {
	switch sub.Status {
	case models.SubscriptionActive, models.SubscriptionTrialing:
	default:
		return false
	}
	return time.Now().Before(sub.CurrentPeriodEnd)
}
