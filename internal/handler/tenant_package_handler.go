package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

type TenantPackageHandler struct {
	svc *service.TenantPackageService
}

func NewTenantPackageHandler(svc *service.TenantPackageService) *TenantPackageHandler {
	return &TenantPackageHandler{svc: svc}
}

// Assign links a catalog package to a tenant's storefront.
func (h *TenantPackageHandler) Assign(c *gin.Context) {
	var body struct {
		PackageID string `json:"package_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Assign(c.Request.Context(), c.Param("id"), body.PackageID, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, gin.H{"assigned": true})
}

func (h *TenantPackageHandler) Unassign(c *gin.Context) {
	if err := h.svc.Unassign(c.Request.Context(), c.Param("id"), c.Param("package_id")); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"unassigned": true})
}

// ListForTenant is the platform admin's view of a tenant's package
// assignments — every assigned package regardless of active status.
func (h *TenantPackageHandler) ListForTenant(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.ListForTenant(c.Request.Context(), c.Param("id"), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}
