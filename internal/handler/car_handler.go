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

type CarHandler struct {
	svc *service.CarService
}

func NewCarHandler(svc *service.CarService) *CarHandler {
	return &CarHandler{svc: svc}
}

func (h *CarHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), tenantID(c), service.ListCarsFilter{
		Type:  c.Query("type"),
		Fuel:  c.Query("fuel"),
		Page:  page,
		Limit: limit,
	})
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *CarHandler) GetBySlug(c *gin.Context) {
	car, err := h.svc.GetBySlug(c.Request.Context(), tenantID(c), c.Param("slug"))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, car)
}

func (h *CarHandler) Create(c *gin.Context) {
	var car models.Car
	if err := c.ShouldBindJSON(&car); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Create(c.Request.Context(), tenantID(c), &car, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, car)
}

func (h *CarHandler) Update(c *gin.Context) {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Update(c.Request.Context(), tenantID(c), c.Param("id"), update, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}

func (h *CarHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), tenantID(c), c.Param("id")); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}
