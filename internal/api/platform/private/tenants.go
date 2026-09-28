package private

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type tenantsController struct {
	svc     *service.TenantService
	userSvc *service.TenantUserService
	quote   *service.QuoteService
}

// Create provisions a tenant along with its API key and, when a contact
// email is given, a bootstrap admin login profile — without it there would
// be no way for the tenant to ever obtain their first admin token. A
// subscription is a separate, explicit step (see subscriptionsController) — a
// tenant with no subscription record yet is not held to any subscription
// state by SubscriptionMiddleware.
func (h *tenantsController) Create(c *gin.Context) error {
	var t models.Tenant
	if err := c.ShouldBindJSON(&t); err != nil {
		return apierr.BadRequest(err.Error())
	}
	created, rawAPIKey, err := h.svc.Create(c.Request.Context(), &t)
	if err != nil {
		return err
	}

	resp := gin.H{"tenant": created, "api_key": rawAPIKey}

	if created.ContactEmail != "" {
		user, rawPassword, err := h.userSvc.Create(c.Request.Context(), created.ID, "", created.ContactEmail, "", models.TenantUserAdmin)
		if err != nil {
			return err
		}
		resp["login"] = gin.H{"user": user, "password": rawPassword}
	}

	response.Created(c, resp)
	return nil
}

func (h *tenantsController) UpdateStatus(c *gin.Context) error {
	var body struct {
		Status models.TenantStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), c.Param("id"), body.Status); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *tenantsController) RotateAPIKey(c *gin.Context) error {
	rawAPIKey, err := h.svc.RotateAPIKey(c.Request.Context(), c.Param("id"))
	if err != nil {
		return err
	}
	response.OK(c, gin.H{"api_key": rawAPIKey})
	return nil
}

// UpdateDomain assigns the domain this tenant's X-API-Key is bound to.
// Once set, TenantMiddleware rejects requests whose Origin/Referer doesn't
// match it, even with a valid key.
func (h *tenantsController) UpdateDomain(c *gin.Context) error {
	var body struct {
		Domain string `json:"domain" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.svc.UpdateDomain(c.Request.Context(), c.Param("id"), body.Domain); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

// UpdateProject edits a tenant's TenantDetail — the "Our Projects"
// case-study content, stored in its own tenant_details table — separately
// from the tenant's identity/billing fields (status, domain, api key),
// which have their own dedicated routes. Also embedded as `project` in GET
// /platform/tenants and /platform/tenants/{id}. Partial update, same as
// Partner/Package: locale-map fields can be set per-locale via dot
// notation, e.g. {"tagline.mn": "..."}.
func (h *tenantsController) UpdateProject(c *gin.Context) error {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.svc.UpdateProject(c.Request.Context(), c.Param("id"), update, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

// ListQuotes is one tenant's own leads, read by the platform. The tenant
// comes from a path param rather than the X-API-Key-derived tenant context,
// since a platform admin holds no tenant API key.
func (h *tenantsController) ListQuotes(c *gin.Context) error {
	tid, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		return apierr.BadRequest("invalid tenant id")
	}

	page, limit := apictx.Page(c, 20)

	data, total, err := h.quote.List(c.Request.Context(), tid, page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}
