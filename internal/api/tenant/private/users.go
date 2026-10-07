package private

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// usersController manages the login profiles inside one tenant. Every call
// is scoped to the tenant the middleware resolved, so a tenant admin cannot
// reach another tenant's users even by guessing an id.
type usersController struct {
	svc *service.TenantUserService
}

func (h *usersController) Create(c *gin.Context) error {
	var body struct {
		Name     string                `json:"name"`
		Email    string                `json:"email" binding:"required"`
		Password string                `json:"password"`
		Role     models.TenantUserRole `json:"role"`
		// ReceiveEmails is optional on create; absent means false.
		ReceiveEmails bool `json:"receive_emails"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}

	u, rawPassword, err := h.svc.Create(c.Request.Context(), apictx.TenantID(c), body.Name, body.Email, body.Password, body.Role, body.ReceiveEmails)
	if err != nil {
		return err
	}

	// A generated password is echoed exactly once, here. It is stored only
	// as a hash and cannot be read back afterwards.
	resp := gin.H{"user": u}
	if body.Password == "" {
		resp["password"] = rawPassword
	}
	response.Created(c, resp)
	return nil
}

func (h *usersController) UpdateStatus(c *gin.Context) error {
	var body struct {
		Status models.TenantUserStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), apictx.TenantID(c), c.Param("id"), body.Status); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

// ResetPassword lets a tenant admin reset another login profile's password
// within their own tenant.
func (h *usersController) ResetPassword(c *gin.Context) error {
	var body struct {
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}

	newPassword, err := h.svc.ResetPassword(c.Request.Context(), apictx.TenantID(c), c.Param("id"), body.NewPassword)
	if err != nil {
		return err
	}

	resp := gin.H{"updated": true}
	if body.NewPassword == "" {
		resp["password"] = newPassword
	}
	response.OK(c, resp)
	return nil
}

// Update changes editable profile fields. receive_emails is a pointer so an
// absent field leaves the stored flag alone instead of resetting it to false.
func (h *usersController) Update(c *gin.Context) error {
	var body struct {
		ReceiveEmails *bool `json:"receive_emails"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.svc.UpdateReceiveEmails(c.Request.Context(), apictx.TenantID(c), c.Param("id"), body.ReceiveEmails); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}
