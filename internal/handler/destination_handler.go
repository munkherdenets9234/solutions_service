package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
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
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *DestinationHandler) GetBySlug(c *gin.Context) {
	d, err := h.svc.GetBySlug(c.Request.Context(), tenantID(c), c.Param("slug"))
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
	if err := h.svc.Create(c.Request.Context(), tenantID(c), &d); err != nil {
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
	if err := h.svc.Update(c.Request.Context(), tenantID(c), c.Param("id"), update); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}

func (h *DestinationHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), tenantID(c), c.Param("id")); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

func handleErr(c *gin.Context, err error) {
	if e, ok := err.(*apierr.APIError); ok {
		response.Error(c, e.StatusCode, e.Message)
		return
	}
	response.Error(c, http.StatusInternalServerError, "internal server error")
}
