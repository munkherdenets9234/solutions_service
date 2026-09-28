package private

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type adminsController struct {
	svc *service.PlatformUserService
}

func (h *adminsController) Create(c *gin.Context) error {
	var body struct {
		Name     string `json:"name"`
		Email    string `json:"email" binding:"required"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}

	u, rawPassword, err := h.svc.Create(c.Request.Context(), body.Name, body.Email, body.Password)
	if err != nil {
		return err
	}

	// A generated password is echoed exactly once, on the response to the
	// request that created it. It is stored only as a hash and cannot be read
	// back afterwards.
	resp := gin.H{"user": u}
	if body.Password == "" {
		resp["password"] = rawPassword
	}
	response.Created(c, resp)
	return nil
}

func (h *adminsController) UpdateStatus(c *gin.Context) error {
	var body struct {
		Status models.PlatformUserStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), c.Param("id"), body.Status); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

// ResetPassword lets a superadmin reset another platform user password.
func (h *adminsController) ResetPassword(c *gin.Context) error {
	var body struct {
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}

	newPassword, err := h.svc.ResetPassword(c.Request.Context(), c.Param("id"), body.NewPassword)
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

// ChangePassword lets the calling platform user change their own password,
// given the current one.
func (h *adminsController) ChangePassword(c *gin.Context) error {
	var body struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}

	// The token verified upstream carries this id, so a parse failure here
	// means we minted a token with a malformed subject: our bug, not the
	// caller's.
	uid, err := primitive.ObjectIDFromHex(apictx.UserID(c))
	if err != nil {
		return apierr.Internal(err)
	}

	if err := h.svc.ChangePassword(c.Request.Context(), uid, body.CurrentPassword, body.NewPassword); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}
