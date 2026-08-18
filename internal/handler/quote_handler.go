package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type QuoteHandler struct {
	svc *service.QuoteService
}

func NewQuoteHandler(svc *service.QuoteService) *QuoteHandler {
	return &QuoteHandler{svc: svc}
}

// Create is the customer-facing "request a quote" submission through a
// specific tenant's own storefront — public beyond the tenant's X-API-Key,
// same as bookings/rentals/contact messages.
func (h *QuoteHandler) Create(c *gin.Context) {
	var q models.Quote
	if err := c.ShouldBindJSON(&q); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	tid := tenantID(c)
	if err := h.svc.Create(c.Request.Context(), &tid, &q); err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, q)
}

// CreatePlatform is the customer-facing "request a quote" submission with
// no existing tenant relationship — a prospect inquiring before ever
// signing up. Fully public: no X-API-Key, no bearer token.
func (h *QuoteHandler) CreatePlatform(c *gin.Context) {
	var q models.Quote
	if err := c.ShouldBindJSON(&q); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Create(c.Request.Context(), nil, &q); err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, q)
}

// List is a single tenant's own leads — the tenant admin panel's view.
func (h *QuoteHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), tenantID(c), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

// ListForTenant is List's platform-superadmin counterpart — same data, but
// the tenant comes from a path param instead of the X-API-Key-derived
// tenant context, since the platform admin has no tenant API key to send.
func (h *QuoteHandler) ListForTenant(c *gin.Context) {
	tid, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid tenant id")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), tid, page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

// ListAll is every lead across the platform, tenant-linked and tenant-less
// alike — the platform admin's consolidated view, mirroring GET
// /platform/packages for the Package catalog.
func (h *QuoteHandler) ListAll(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.ListAll(c.Request.Context(), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

// UpdateStatus is the tenant admin panel's status update — scoped to leads
// that actually belong to this tenant.
func (h *QuoteHandler) UpdateStatus(c *gin.Context) {
	var body struct {
		Status models.QuoteStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.UpdateStatusForTenant(c.Request.Context(), tenantID(c), c.Param("id"), body.Status, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}

// UpdateStatusPlatform is UpdateStatus's platform-superadmin counterpart —
// works on any quote regardless of tenant, the only way to act on a
// tenant-less lead.
func (h *QuoteHandler) UpdateStatusPlatform(c *gin.Context) {
	var body struct {
		Status models.QuoteStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), c.Param("id"), body.Status, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}
