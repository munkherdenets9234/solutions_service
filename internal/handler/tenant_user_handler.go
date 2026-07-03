package handler

import (
	"net/http"
	"strconv"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

type TenantUserHandler struct {
	svc *service.TenantUserService
}

func NewTenantUserHandler(svc *service.TenantUserService) *TenantUserHandler {
	return &TenantUserHandler{svc: svc}
}

func (h *TenantUserHandler) Create(c *gin.Context) {
	var body struct {
		Name     string                `json:"name"`
		Email    string                `json:"email" binding:"required"`
		Password string                `json:"password"`
		Role     models.TenantUserRole `json:"role"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	u, rawPassword, err := h.svc.Create(c.Request.Context(), tenantID(c), body.Name, body.Email, body.Password, body.Role)
	if err != nil {
		handleErr(c, err)
		return
	}

	resp := gin.H{"user": u}
	if body.Password == "" {
		resp["password"] = rawPassword
	}
	response.Created(c, resp)
}

func (h *TenantUserHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), tenantID(c), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *TenantUserHandler) UpdateStatus(c *gin.Context) {
	var body struct {
		Status models.TenantUserStatus `json:"status" binding:"required"`
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

func (h *TenantUserHandler) Login(c *gin.Context) {
	var body struct {
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	tok, err := h.svc.Login(c.Request.Context(), tenantID(c), body.Email, body.Password)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"token": tok})
}
