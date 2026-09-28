package private

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// accountController is what a tenant user may do to their own account,
// whatever their role.
type accountController struct {
	svc *service.TenantUserService
}

// ChangePassword changes the calling user's own password, given the current
// one.
func (h *accountController) ChangePassword(c *gin.Context) error {
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

	if err := h.svc.ChangePassword(c.Request.Context(), apictx.TenantID(c), uid, body.CurrentPassword, body.NewPassword); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}
