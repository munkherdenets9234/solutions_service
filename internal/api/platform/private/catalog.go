package private

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

// catalogController owns every write to the platform catalog: the package
// price list, its per-tenant assignments, the testimonials, and the status of
// leads.
type catalogController struct {
	pkg           *service.PackageService
	tenantPackage *service.TenantPackageService
	review        *service.TenantReviewService
	quote         *service.QuoteService
}

func (h *catalogController) CreatePackage(c *gin.Context) error {
	var p models.Package
	if err := c.ShouldBindJSON(&p); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.pkg.Create(c.Request.Context(), &p, apictx.ActorID(c)); err != nil {
		return err
	}
	response.Created(c, p)
	return nil
}

func (h *catalogController) UpdatePackage(c *gin.Context) error {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.pkg.Update(c.Request.Context(), c.Param("id"), update, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *catalogController) DeletePackage(c *gin.Context) error {
	if err := h.pkg.Delete(c.Request.Context(), c.Param("id"), apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"deleted": true})
	return nil
}

// AssignPackage links a catalog package to one tenant storefront.
func (h *catalogController) AssignPackage(c *gin.Context) error {
	var body struct {
		PackageID string `json:"package_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.tenantPackage.Assign(c.Request.Context(), c.Param("id"), body.PackageID, apictx.ActorID(c)); err != nil {
		return err
	}
	response.Created(c, gin.H{"assigned": true})
	return nil
}

func (h *catalogController) UnassignPackage(c *gin.Context) error {
	if err := h.tenantPackage.Unassign(c.Request.Context(), c.Param("id"), c.Param("package_id")); err != nil {
		return err
	}
	response.OK(c, gin.H{"unassigned": true})
	return nil
}

func (h *catalogController) CreateReview(c *gin.Context) error {
	var rev models.TenantReview
	if err := c.ShouldBindJSON(&rev); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.review.Create(c.Request.Context(), &rev, apictx.ActorID(c)); err != nil {
		return err
	}
	response.Created(c, rev)
	return nil
}

func (h *catalogController) UpdateReview(c *gin.Context) error {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.review.Update(c.Request.Context(), c.Param("id"), update, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *catalogController) DeleteReview(c *gin.Context) error {
	if err := h.review.Delete(c.Request.Context(), c.Param("id"), apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"deleted": true})
	return nil
}

// UpdateQuoteStatus works on any quote regardless of tenant — the only way to
// act on a tenant-less lead. Its tenant-scoped counterpart lives in
// tenant/private and can only touch leads belonging to the calling tenant.
func (h *catalogController) UpdateQuoteStatus(c *gin.Context) error {
	var body struct {
		Status models.QuoteStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.quote.UpdateStatus(c.Request.Context(), c.Param("id"), body.Status, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}
