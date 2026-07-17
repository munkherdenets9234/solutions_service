package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

type ContactMessageHandler struct {
	svc *service.ContactMessageService
}

func NewContactMessageHandler(svc *service.ContactMessageService) *ContactMessageHandler {
	return &ContactMessageHandler{svc: svc}
}

func (h *ContactMessageHandler) Create(c *gin.Context) {
	var m models.ContactMessage
	if err := c.ShouldBindJSON(&m); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Create(c.Request.Context(), tenantID(c), &m); err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, m)
}

func (h *ContactMessageHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), tenantID(c), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *ContactMessageHandler) UpdateStatus(c *gin.Context) {
	var body struct {
		Status models.ContactStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), tenantID(c), c.Param("id"), body.Status, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}
