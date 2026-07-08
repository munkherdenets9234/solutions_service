package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

type ReviewHandler struct {
	svc *service.ReviewService
}

func NewReviewHandler(svc *service.ReviewService) *ReviewHandler {
	return &ReviewHandler{svc: svc}
}

func (h *ReviewHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), tenantID(c), service.ListReviewsFilter{
		Tour:    c.Query("tour"),
		Partner: c.Query("partner"),
		Page:    page,
		Limit:   limit,
	})
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *ReviewHandler) GetByID(c *gin.Context) {
	rev, err := h.svc.GetByID(c.Request.Context(), tenantID(c), c.Param("id"))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, rev)
}

func (h *ReviewHandler) Create(c *gin.Context) {
	var rev models.Review
	if err := c.ShouldBindJSON(&rev); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Create(c.Request.Context(), tenantID(c), &rev); err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, rev)
}

func (h *ReviewHandler) Update(c *gin.Context) {
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

func (h *ReviewHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), tenantID(c), c.Param("id")); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}
