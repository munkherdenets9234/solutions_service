// Package tenantcore holds the platform routes that tenantcore's operators
// reach directly, authenticated by a tenantcore-signed Ed25519 token rather
// than this service's own HMAC one. Register applies that guard itself, on a
// group of its own, so nothing here can be mounted without it.
package tenantcore

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/httpx"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Users is the tenant-user reads the controller needs.
type Users interface {
	ListAdmins(ctx context.Context, tenantID primitive.ObjectID) ([]*models.TenantUser, error)
	GetAdmin(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*models.TenantUser, error)
}

// Resetter starts the emailed-code reset (TenantPasswordResetService).
type Resetter interface {
	Request(ctx context.Context, tenantID primitive.ObjectID, email string) error
}

// Deps bundles what the group needs.
type Deps struct {
	Auth      *middleware.TenantcoreAuth
	Users     Users
	Reset     Resetter
	RateLimit gin.HandlerFunc
}

type adminUsersController struct {
	users Users
	reset Resetter
}

// Register mounts the routes onto base (already prefixed with /platform).
//
// The guard sits on a Group of its own at /tenants/:id. That path is shared by
// public routes (GET /platform/tenants/:id and .../packages); a separate Group
// means the guard covers only the paths registered here and the public ones
// are untouched. With no verifier configured the group answers 404.
func Register(base *gin.RouterGroup, d Deps) {
	h := &adminUsersController{users: d.Users, reset: d.Reset}
	limit := d.RateLimit
	if limit == nil {
		limit = func(c *gin.Context) { c.Next() }
	}

	g := base.Group("/tenants/:id", d.Auth.RequireSuperadmin())
	g.GET("/admin-users", httpx.H(h.List))
	g.POST("/admin-users/:user_id/reset-password", limit, httpx.H(h.ResetPassword))
}

type adminUserDTO struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

func tenantID(c *gin.Context) (primitive.ObjectID, error) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		return primitive.NilObjectID, apierr.BadRequest("invalid tenant id")
	}
	return id, nil
}

func (h *adminUsersController) List(c *gin.Context) error {
	tid, err := tenantID(c)
	if err != nil {
		return err
	}
	users, err := h.users.ListAdmins(c.Request.Context(), tid)
	if err != nil {
		return err
	}
	out := make([]adminUserDTO, 0, len(users))
	for _, u := range users {
		out = append(out, adminUserDTO{ID: u.ID.Hex(), Email: u.Email, Name: u.Name, Status: string(u.Status)})
	}
	response.OK(c, out)
	return nil
}

// ResetPassword starts the emailed-code flow for one admin user. The address
// comes from the stored user, never from the caller, and the response carries
// no code and no address.
func (h *adminUsersController) ResetPassword(c *gin.Context) error {
	tid, err := tenantID(c)
	if err != nil {
		return err
	}
	u, err := h.users.GetAdmin(c.Request.Context(), tid, c.Param("user_id"))
	if err != nil {
		return err
	}
	if u.Status != models.TenantUserActive {
		return apierr.Conflict("this admin account is suspended")
	}
	if err := h.reset.Request(c.Request.Context(), tid, u.Email); err != nil {
		return err
	}
	response.OK(c, gin.H{"message": "A reset code was emailed."})
	return nil
}
