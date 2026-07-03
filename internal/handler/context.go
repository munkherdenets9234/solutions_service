package handler

import (
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// tenantID reads the tenant resolved by middleware.TenantMiddleware. Only
// call this from routes mounted behind that middleware.
func tenantID(c *gin.Context) primitive.ObjectID {
	return c.MustGet("tenant_id").(primitive.ObjectID)
}
