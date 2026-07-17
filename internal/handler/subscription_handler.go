package handler

import (
	"net/http"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type SubscriptionHandler struct {
	svc *service.SubscriptionService
}

func NewSubscriptionHandler(svc *service.SubscriptionService) *SubscriptionHandler {
	return &SubscriptionHandler{svc: svc}
}

func (h *SubscriptionHandler) Create(c *gin.Context) {
	tenantID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid tenant id")
		return
	}

	var body struct {
		Plan models.SubscriptionPlan `json:"plan"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	sub, err := h.svc.Create(c.Request.Context(), tenantID, body.Plan, currentUserID(c))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, sub)
}

func (h *SubscriptionHandler) Get(c *gin.Context) {
	tenantID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid tenant id")
		return
	}

	sub, err := h.svc.Get(c.Request.Context(), tenantID)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, sub)
}

func (h *SubscriptionHandler) UpdatePlan(c *gin.Context) {
	tenantID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid tenant id")
		return
	}

	var body struct {
		Plan models.SubscriptionPlan `json:"plan" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.svc.UpdatePlan(c.Request.Context(), tenantID, body.Plan, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}

func (h *SubscriptionHandler) Cancel(c *gin.Context) {
	tenantID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid tenant id")
		return
	}

	if err := h.svc.Cancel(c.Request.Context(), tenantID, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"canceled": true})
}
