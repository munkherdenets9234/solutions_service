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

type TenantHandler struct {
	svc     *service.TenantService
	userSvc *service.TenantUserService
}

func NewTenantHandler(svc *service.TenantService, userSvc *service.TenantUserService) *TenantHandler {
	return &TenantHandler{svc: svc, userSvc: userSvc}
}

// Create provisions a tenant along with its API key and, when a contact
// email is given, a bootstrap admin login profile — without it there would
// be no way for the tenant to ever obtain their first admin token. A
// subscription is a separate, explicit step (see SubscriptionHandler) — a
// tenant with no subscription record yet is not held to any subscription
// state by SubscriptionMiddleware.
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

// UpdateDomain assigns the domain this tenant's X-API-Key is bound to.
// Once set, TenantMiddleware rejects requests whose Origin/Referer doesn't
// match it, even with a valid key.
func (h *TenantHandler) UpdateDomain(c *gin.Context) {
	var body struct {
		Domain string `json:"domain" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.UpdateDomain(c.Request.Context(), c.Param("id"), body.Domain); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}

// ListProjects is the public "Our Projects" listing — active tenants with a
// showcase-enabled TenantDetail, locale-resolved.
func (h *TenantHandler) ListProjects(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	tenants, details, total, err := h.svc.ListProjects(c.Request.Context(), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	locale := i18n.ResolveFromRequest(c)
	response.List(c, dto.ToProjectResponses(tenants, details, locale), response.Meta{Total: total, Page: page, Limit: limit})
}

// GetProjectBySlug is ListProjects' single-project counterpart, for the
// case-study detail page.
func (h *TenantHandler) GetProjectBySlug(c *gin.Context) {
	t, d, err := h.svc.GetProjectBySlug(c.Request.Context(), c.Param("slug"))
	if err != nil {
		handleErr(c, err)
		return
	}
	locale := i18n.ResolveFromRequest(c)
	response.OK(c, dto.ToProjectResponse(t, d, locale))
}

// UpdateProject edits a tenant's TenantDetail — the "Our Projects"
// case-study content, stored in its own tenant_details table — separately
// from the tenant's identity/billing fields (status, domain, api key),
// which have their own dedicated routes. Also embedded as `project` in GET
// /platform/tenants and /platform/tenants/{id}. Partial update, same as
// Partner/Package: locale-map fields can be set per-locale via dot
// notation, e.g. {"tagline.mn": "..."}.
func (h *TenantHandler) UpdateProject(c *gin.Context) {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.UpdateProject(c.Request.Context(), c.Param("id"), update, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"updated": true})
}
