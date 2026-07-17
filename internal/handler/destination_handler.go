package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/dto"
	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/logger"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

type DestinationHandler struct {
	svc *service.DestinationService
}

func NewDestinationHandler(svc *service.DestinationService) *DestinationHandler {
	return &DestinationHandler{svc: svc}
}

func (h *DestinationHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	var featured *bool
	if raw := c.Query("featured"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, "invalid featured value")
			return
		}
		featured = &b
	}

	data, total, err := h.svc.List(c.Request.Context(), tenantID(c), service.ListDestinationsFilter{
		Category: c.Query("category"),
		Region:   c.Query("region"),
		Season:   c.Query("season"),
		Featured: featured,
		Page:     page,
		Limit:    limit,
	})
	if err != nil {
		handleErr(c, err)
		return
	}
	locale := i18n.ResolveFromRequest(c)
	response.List(c, dto.ToDestinationResponses(data, locale), response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *DestinationHandler) GetBySlug(c *gin.Context) {
	d, err := h.svc.GetBySlug(c.Request.Context(), tenantID(c), c.Param("slug"))
	if err != nil {
		handleErr(c, err)
		return
	}
	locale := i18n.ResolveFromRequest(c)
	response.OK(c, dto.ToDestinationResponse(d, locale))
}

// ListAdmin returns every destination for a tenant (active and inactive)
// with full locale maps intact, for the admin CMS to edit every language at once.
func (h *DestinationHandler) ListAdmin(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.ListAdmin(c.Request.Context(), tenantID(c), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

// GetByID returns a single destination with full locale maps intact, for
// admin edit forms.
func (h *DestinationHandler) GetByID(c *gin.Context) {
	d, err := h.svc.GetByID(c.Request.Context(), tenantID(c), c.Param("id"))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, d)
}

func (h *DestinationHandler) Create(c *gin.Context) {
	var d models.Destination
	if err := c.ShouldBindJSON(&d); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Create(c.Request.Context(), tenantID(c), &d, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, d)
}

func (h *DestinationHandler) Update(c *gin.Context) {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Update(c.Request.Context(), tenantID(c), c.Param("id"), update, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}

func (h *DestinationHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), tenantID(c), c.Param("id"), currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

// handleErr is shared by every handler in this package. Known, expected
// failures (*apierr.APIError) are reported to the client as-is. Anything
// else is unexpected — it's logged with the full error and request path so
// it shows up in the server logs instead of only ever surfacing to the
// client as an opaque "internal server error".
func handleErr(c *gin.Context, err error) {
	if e, ok := err.(*apierr.APIError); ok {
		response.Error(c, e.StatusCode, e.Message)
		return
	}
	logger.Log.Error("unhandled error", zap.Error(err), zap.String("path", c.FullPath()))
	response.Error(c, http.StatusInternalServerError, "internal server error")
}
