package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

type TenantHandler struct {
	svc     *service.TenantService
	userSvc *service.TenantUserService
}

func NewTenantHandler(svc *service.TenantService, userSvc *service.TenantUserService) *TenantHandler {
	return &TenantHandler{svc: svc, userSvc: userSvc}
}

// Create provisions a tenant along with its API key and, when a contact
// email is given, a bootstrap admin login profile — without it there would
// be no way for the tenant to ever obtain their first admin token.
func (h *TenantHandler) Create(c *gin.Context) {
	var t models.Tenant
	if err := c.ShouldBindJSON(&t); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	created, rawAPIKey, err := h.svc.Create(c.Request.Context(), &t)
	if err != nil {
		handleErr(c, err)
		return
	}

	resp := gin.H{"tenant": created, "api_key": rawAPIKey}

	if created.ContactEmail != "" {
		user, rawPassword, err := h.userSvc.Create(c.Request.Context(), created.ID, "", created.ContactEmail, "", models.TenantUserAdmin)
		if err != nil {
			handleErr(c, err)
			return
		}
		resp["login"] = gin.H{"user": user, "password": rawPassword}
	}

	response.Created(c, resp)
}

func (h *TenantHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *TenantHandler) GetByID(c *gin.Context) {
	t, err := h.svc.GetByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, t)
}

func (h *TenantHandler) UpdateStatus(c *gin.Context) {
	var body struct {
		Status models.TenantStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), c.Param("id"), body.Status); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}

func (h *TenantHandler) RotateAPIKey(c *gin.Context) {
	rawAPIKey, err := h.svc.RotateAPIKey(c.Request.Context(), c.Param("id"))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"api_key": rawAPIKey})
}
