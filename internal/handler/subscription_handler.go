package handler

import (
	"net/http"

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
		PackageID string `json:"package_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	packageID, err := primitive.ObjectIDFromHex(body.PackageID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid package_id")
		return
	}

	sub, err := h.svc.Create(c.Request.Context(), tenantID, packageID, currentUserID(c))
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

func (h *SubscriptionHandler) UpdatePackage(c *gin.Context) {
	tenantID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid tenant id")
		return
	}

	var body struct {
		PackageID string `json:"package_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	packageID, err := primitive.ObjectIDFromHex(body.PackageID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid package_id")
		return
	}

	if err := h.svc.UpdatePackage(c.Request.Context(), tenantID, packageID, currentUserID(c)); err != nil {
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
