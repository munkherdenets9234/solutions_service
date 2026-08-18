package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/dto"
	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

type TenantReviewHandler struct {
	svc *service.TenantReviewService
}

func NewTenantReviewHandler(svc *service.TenantReviewService) *TenantReviewHandler {
	return &TenantReviewHandler{svc: svc}
}

// List is the public "get all" testimonials listing — active tenant reviews
// only, resolved to a single locale.
func (h *TenantReviewHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	locale := i18n.ResolveFromRequest(c)
	response.List(c, dto.ToTenantReviewResponses(data, locale), response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *TenantReviewHandler) Create(c *gin.Context) {
	var rev models.TenantReview
	if err := c.ShouldBindJSON(&rev); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Create(c.Request.Context(), &rev, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, rev)
}

func (h *TenantReviewHandler) Update(c *gin.Context) {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Update(c.Request.Context(), c.Param("id"), update, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}

func (h *TenantReviewHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id"), currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}
