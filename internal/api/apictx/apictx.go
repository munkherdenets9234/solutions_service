// Package apictx reads the values middleware puts on the request context.
//
// Every one of these is set by a specific middleware, so every accessor is
// only valid on routes mounted behind that middleware. That constraint is why
// they live here rather than being inlined: one place to state which
// middleware each value depends on, and one place to change if a key ever
// moves.
package apictx

import (
	"strconv"

	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TenantID reads the tenant resolved by middleware.TenantMiddleware. Only
// call it from routes mounted behind that middleware — it panics otherwise,
// deliberately: a controller reading a tenant id on a route that never
// resolved one is a routing bug, and a zero ObjectID would quietly scope the
// query to nothing instead of saying so.
func TenantID(c *gin.Context) primitive.ObjectID {
	return c.MustGet(middleware.CtxTenantID).(primitive.ObjectID)
}

// UserID reads the caller's own user id, set by middleware.AuthMiddleware
// from the bearer token's claims. Only call it from routes mounted behind
// that middleware.
func UserID(c *gin.Context) string {
	return c.MustGet(middleware.CtxUserID).(string)
}

// ActorID is UserID parsed as an ObjectID, for stamping the audit link
// (user_id) on writes. Returns nil when the route has no authenticated user,
// which is the case on every public write — a lead form has no actor, and
// recording one would be a lie.
func ActorID(c *gin.Context) *primitive.ObjectID {
	raw, ok := c.Get(middleware.CtxUserID)
	if !ok {
		return nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil
	}
	id, err := primitive.ObjectIDFromHex(s)
	if err != nil {
		return nil
	}
	return &id
}

// Page reads the page/limit query pair, falling back to the given defaults.
// Every list endpoint used to parse these itself with a discarded error, so
// ?page=abc silently became page 0 on some routes and page 1 on others.
func Page(c *gin.Context, defaultLimit int) (page, limit int) {
	page = atoiOr(c.Query("page"), 1)
	limit = atoiOr(c.Query("limit"), defaultLimit)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = defaultLimit
	}
	return page, limit
}

func atoiOr(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}
