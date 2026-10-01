package public

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// passwordResetController is the emailed-code reset for a tenant's admin users.
//
// It is reachable without a session because it has to be: someone who has
// forgotten their password has none. It is NOT reachable without a tenant. Like
// every route here it sits behind the X-API-Key check, and the tenant it
// resolves is the one the code is issued for and looked up in, which is what
// keeps one tenant's reset from touching another's.
//
// The rules that keep it from becoming an account-existence oracle live in the
// service, not here, so no edit to a controller can weaken them.
type passwordResetController struct {
	svc *service.TenantPasswordResetService
}

// Request mails a one-time code to the address, if it belongs to an active user
// of this tenant.
//
// Always 200. An unknown address, a suspended user and a failed send are
// indistinguishable to the caller, in the body and in the time taken. The one
// exception is 503 when mail is not configured on this deployment, which says
// nothing about the account.
func (h *passwordResetController) Request(c *gin.Context) error {
	var body struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}

	if err := h.svc.Request(c.Request.Context(), apictx.TenantID(c), body.Email); err != nil {
		return err
	}

	response.OK(c, gin.H{
		"sent": true,
		// Phrased so it is true whether or not an account exists.
		"message": "If that address belongs to an account, a reset code is on its way.",
	})
	return nil
}

// Confirm verifies the code and sets the new password.
func (h *passwordResetController) Confirm(c *gin.Context) error {
	var body struct {
		Email       string `json:"email" binding:"required,email"`
		Code        string `json:"code" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}

	if err := h.svc.Confirm(c.Request.Context(), apictx.TenantID(c), body.Email, body.Code, body.NewPassword); err != nil {
		return err
	}

	// No token is issued. Signing in afterwards is one extra step and it keeps
	// this from being a second way to mint a session: a reset that hands back a
	// live token turns any weakness in it directly into account access.
	response.OK(c, gin.H{"reset": true})
	return nil
}
