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

type PackageHandler struct {
	svc *service.PackageService
}

func NewPackageHandler(svc *service.PackageService) *PackageHandler {
	return &PackageHandler{svc: svc}
}

// List is the tenant-scoped storefront price list — packages assigned to
// this tenant (see TenantPackage) via its X-API-Key.
func (h *PackageHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.ListForTenant(c.Request.Context(), tenantID(c), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	locale := i18n.ResolveFromRequest(c)
	response.List(c, dto.ToPackageResponses(data, locale), response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *PackageHandler) GetBySlug(c *gin.Context) {
	p, err := h.svc.GetBySlugForTenant(c.Request.Context(), tenantID(c), c.Param("slug"))
	if err != nil {
		handleErr(c, err)
		return
	}
	locale := i18n.ResolveFromRequest(c)
	response.OK(c, dto.ToPackageResponse(p, locale))
}

// ListAll returns every package in the platform's catalog, active and
// inactive, with full locale maps intact — the platform admin's management
// view of the whole price list, not scoped to any one tenant.
func (h *PackageHandler) ListAll(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.ListAll(c.Request.Context(), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

// GetByID returns a single catalog package with full locale maps intact, for
// the platform admin's edit form.
func (h *PackageHandler) GetByID(c *gin.Context) {
	p, err := h.svc.GetByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, p)
}

func (h *PackageHandler) Create(c *gin.Context) {
	var p models.Package
	if err := c.ShouldBindJSON(&p); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Create(c.Request.Context(), &p, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, p)
}

func (h *PackageHandler) Update(c *gin.Context) {
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

func (h *PackageHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id"), currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}
