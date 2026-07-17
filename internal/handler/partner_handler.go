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

type PartnerHandler struct {
	svc *service.PartnerService
}

func NewPartnerHandler(svc *service.PartnerService) *PartnerHandler {
	return &PartnerHandler{svc: svc}
}

func (h *PartnerHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.List(c.Request.Context(), tenantID(c), service.ListPartnersFilter{
		Tag:   c.Query("tag"),
		Page:  page,
		Limit: limit,
	})
	if err != nil {
		handleErr(c, err)
		return
	}
	locale := i18n.ResolveFromRequest(c)
	response.List(c, dto.ToPartnerResponses(data, locale), response.Meta{Total: total, Page: page, Limit: limit})
}

func (h *PartnerHandler) GetBySlug(c *gin.Context) {
	p, err := h.svc.GetBySlug(c.Request.Context(), tenantID(c), c.Param("slug"))
	if err != nil {
		handleErr(c, err)
		return
	}
	locale := i18n.ResolveFromRequest(c)
	response.OK(c, dto.ToPartnerResponse(p, locale))
}

// ListAdmin returns every partner for a tenant (active and inactive) with
// full locale maps intact, for the admin CMS to edit every language at once.
func (h *PartnerHandler) ListAdmin(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	data, total, err := h.svc.ListAdmin(c.Request.Context(), tenantID(c), page, limit)
	if err != nil {
		handleErr(c, err)
		return
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
}

// GetByID returns a single partner with full locale maps intact, for admin
// edit forms.
func (h *PartnerHandler) GetByID(c *gin.Context) {
	p, err := h.svc.GetByID(c.Request.Context(), tenantID(c), c.Param("id"))
	if err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, p)
}

func (h *PartnerHandler) Create(c *gin.Context) {
	var p models.Partner
	if err := c.ShouldBindJSON(&p); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Create(c.Request.Context(), tenantID(c), &p, currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.Created(c, p)
}

func (h *PartnerHandler) Update(c *gin.Context) {
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

func (h *PartnerHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), tenantID(c), c.Param("id"), currentUserID(c)); err != nil {
		handleErr(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}
