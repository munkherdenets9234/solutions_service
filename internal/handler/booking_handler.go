package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

type BookingHandler struct {
	svc *service.BookingService
}

func NewBookingHandler(svc *service.BookingService) *BookingHandler {
	return &BookingHandler{svc: svc}
}

type createBookingRequest struct {
	DestinationID string          `json:"destination_id" binding:"required"`
	Customer      models.Customer `json:"customer" binding:"required"`
	Booking       models.Booking  `json:"booking" binding:"required"`
}

func (h *BookingHandler) Create(c *gin.Context) {
	var req createBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	b, err := h.svc.Create(c.Request.Context(), tenantID(c), service.CreateBookingInput{
		DestinationID: req.DestinationID,
		Customer:      req.Customer,
		Booking:       req.Booking,
	})
	if err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, b)
}

func (h *BookingHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), tenantID(c), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *BookingHandler) GetByID(c *gin.Context) {
	b, err := h.svc.GetByID(c.Request.Context(), tenantID(c), c.Param("id"))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, b)
}

func (h *BookingHandler) UpdateStatus(c *gin.Context) {
	var body struct {
		Status models.BookingStatus `json:"status" binding:"required"`
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
