package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

type AirportTransferHandler struct {
	svc *service.AirportTransferService
}

func NewAirportTransferHandler(svc *service.AirportTransferService) *AirportTransferHandler {
	return &AirportTransferHandler{svc: svc}
}

type createTransferRequest struct {
	Customer models.Customer        `json:"customer" binding:"required"`
	Transfer models.AirportTransfer `json:"transfer" binding:"required"`
}

func (h *AirportTransferHandler) Create(c *gin.Context) {
	var req createTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	t, err := h.svc.Create(c.Request.Context(), tenantID(c), service.CreateTransferInput{
		Customer: req.Customer,
		Transfer: req.Transfer,
	})
	if err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, t)
}

func (h *AirportTransferHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), tenantID(c), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *AirportTransferHandler) GetByID(c *gin.Context) {
	t, err := h.svc.GetByID(c.Request.Context(), tenantID(c), c.Param("id"))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, t)
}

func (h *AirportTransferHandler) UpdateStatus(c *gin.Context) {
	var body struct {
		Status models.TransferStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), tenantID(c), c.Param("id"), body.Status); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}
