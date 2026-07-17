package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

type RentalHandler struct {
	svc *service.RentalService
}

func NewRentalHandler(svc *service.RentalService) *RentalHandler {
	return &RentalHandler{svc: svc}
}

type createRentalRequest struct {
	CarID    string          `json:"car_id" binding:"required"`
	Customer models.Customer `json:"customer" binding:"required"`
	Rental   models.Rental   `json:"rental" binding:"required"`
}

func (h *RentalHandler) Create(c *gin.Context) {
	var req createRentalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	rt, err := h.svc.Create(c.Request.Context(), tenantID(c), service.CreateRentalInput{
		CarID:    req.CarID,
		Customer: req.Customer,
		Rental:   req.Rental,
	})
	if err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, rt)
}

func (h *RentalHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), tenantID(c), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *RentalHandler) GetByID(c *gin.Context) {
	rt, err := h.svc.GetByID(c.Request.Context(), tenantID(c), c.Param("id"))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, rt)
}

func (h *RentalHandler) UpdateStatus(c *gin.Context) {
	var body struct {
		Status models.RentalStatus `json:"status" binding:"required"`
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
