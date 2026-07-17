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

// userID reads the caller's own user id, set by middleware.AuthMiddleware
// from the bearer token's claims. Only call this from routes mounted behind
// that middleware.
func userID(c *gin.Context) string {
	return c.MustGet("user_id").(string)
}

// currentUserID is userID(c) parsed as an ObjectID, for stamping the
// tenant_users audit link (user_id) on writes. Only call this from routes
// mounted behind AuthMiddleware.
func currentUserID(c *gin.Context) *primitive.ObjectID {
	id, err := primitive.ObjectIDFromHex(userID(c))
	if err != nil {
		return nil
	}
	return &id
}
