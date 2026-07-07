package handler

import (
	"strconv"

	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

type CustomerHandler struct {
	svc *service.CustomerService
}

func NewCustomerHandler(svc *service.CustomerService) *CustomerHandler {
	return &CustomerHandler{svc: svc}
}

func (h *CustomerHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), tenantID(c), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *CustomerHandler) GetByID(c *gin.Context) {
	data, err := h.svc.GetByID(c.Request.Context(), tenantID(c), c.Param("id"))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, data)
}
