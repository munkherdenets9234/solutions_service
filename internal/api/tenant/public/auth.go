package public

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// authController issues tenant user tokens. It is public because it has to
// be: this is where a tenant admin obtains the token every private route
// then demands.
type authController struct {
	svc *service.TenantUserService
}

func (h *authController) Login(c *gin.Context) error {
	var body struct {
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}

	tok, err := h.svc.Login(c.Request.Context(), apictx.TenantID(c), body.Email, body.Password)
	if err != nil {
		return err
	}
	response.OK(c, gin.H{"token": tok})
	return nil
}
