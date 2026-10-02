package private

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// uploadsController accepts images for a tenant's own content.
//
// svc is nil when CLOUDINARY_URL was not configured. The route stays mounted
// in that case and answers 503 FEATURE_UNAVAILABLE rather than 404: a client
// can then tell "this deployment has no image host" from "you called the
// wrong URL", which a 404 cannot express and which is exactly the confusion
// that lets a silently disabled feature go unnoticed.
type uploadsController struct {
	svc *service.UploadService
}

func (h *uploadsController) Upload(c *gin.Context) error {
	if !h.svc.Available() {
		return apierr.FeatureUnavailable("image uploads")
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		return apierr.BadRequest("file is required")
	}

	file, err := fileHeader.Open()
	if err != nil {
		return apierr.BadRequest("could not read file")
	}
	defer file.Close()

	// The destination folder comes from the resolved tenant, never from the
	// request. See service.UploadService.Upload for why.
	result, err := h.svc.Upload(c.Request.Context(), file, apictx.TenantID(c))
	if err != nil {
		return err
	}
	response.Created(c, result)
	return nil
}
