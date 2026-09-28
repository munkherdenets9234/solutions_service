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

// contentController owns every write to a tenant's own published content:
// destinations, blogs, cars, partners and curated reviews.
//
// Updates take a raw bson.M rather than a typed struct so the CMS can send a
// partial document, including dot-notation keys that set one locale of a
// locale map ("tagline.mn"). The service layer is what decides which keys are
// writable; accepting the map here does not mean accepting every key.
type contentController struct {
	destination *service.DestinationService
	blog        *service.BlogService
	car         *service.CarService
	review      *service.ReviewService
	partner     *service.PartnerService
}

// ── Destinations ──────────────────────────────────────────────────────────

func (h *contentController) CreateDestination(c *gin.Context) error {
	var d models.Destination
	if err := c.ShouldBindJSON(&d); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.destination.Create(c.Request.Context(), apictx.TenantID(c), &d, apictx.ActorID(c)); err != nil {
		return err
	}
	response.Created(c, d)
	return nil
}

func (h *contentController) UpdateDestination(c *gin.Context) error {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.destination.Update(c.Request.Context(), apictx.TenantID(c), c.Param("id"), update, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *contentController) DeleteDestination(c *gin.Context) error {
	if err := h.destination.Delete(c.Request.Context(), apictx.TenantID(c), c.Param("id"), apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"deleted": true})
	return nil
}

// ── Blogs ─────────────────────────────────────────────────────────────────

func (h *contentController) CreateBlog(c *gin.Context) error {
	var b models.Blog
	if err := c.ShouldBindJSON(&b); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.blog.Create(c.Request.Context(), apictx.TenantID(c), &b, apictx.ActorID(c)); err != nil {
		return err
	}
	response.Created(c, b)
	return nil
}

func (h *contentController) UpdateBlog(c *gin.Context) error {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.blog.Update(c.Request.Context(), apictx.TenantID(c), c.Param("id"), update, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *contentController) PublishBlog(c *gin.Context) error {
	if err := h.blog.Publish(c.Request.Context(), apictx.TenantID(c), c.Param("id"), apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"published": true})
	return nil
}

// ── Cars ──────────────────────────────────────────────────────────────────

func (h *contentController) CreateCar(c *gin.Context) error {
	var car models.Car
	if err := c.ShouldBindJSON(&car); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.car.Create(c.Request.Context(), apictx.TenantID(c), &car, apictx.ActorID(c)); err != nil {
		return err
	}
	response.Created(c, car)
	return nil
}

func (h *contentController) UpdateCar(c *gin.Context) error {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.car.Update(c.Request.Context(), apictx.TenantID(c), c.Param("id"), update, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *contentController) DeleteCar(c *gin.Context) error {
	if err := h.car.Delete(c.Request.Context(), apictx.TenantID(c), c.Param("id"), apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"deleted": true})
	return nil
}

// ── Partners ──────────────────────────────────────────────────────────────

func (h *contentController) CreatePartner(c *gin.Context) error {
	var p models.Partner
	if err := c.ShouldBindJSON(&p); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.partner.Create(c.Request.Context(), apictx.TenantID(c), &p, apictx.ActorID(c)); err != nil {
		return err
	}
	response.Created(c, p)
	return nil
}

func (h *contentController) UpdatePartner(c *gin.Context) error {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.partner.Update(c.Request.Context(), apictx.TenantID(c), c.Param("id"), update, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *contentController) DeletePartner(c *gin.Context) error {
	if err := h.partner.Delete(c.Request.Context(), apictx.TenantID(c), c.Param("id"), apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"deleted": true})
	return nil
}

// ── Reviews ───────────────────────────────────────────────────────────────

// CreateReview is the admin's own multi-locale review entry, distinct from
// the storefront's single-locale public submission.
func (h *contentController) CreateReview(c *gin.Context) error {
	var rev models.Review
	if err := c.ShouldBindJSON(&rev); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.review.Create(c.Request.Context(), apictx.TenantID(c), &rev, apictx.ActorID(c)); err != nil {
		return err
	}
	response.Created(c, rev)
	return nil
}

func (h *contentController) UpdateReview(c *gin.Context) error {
	var update bson.M
	if err := c.ShouldBindJSON(&update); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.review.Update(c.Request.Context(), apictx.TenantID(c), c.Param("id"), update, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *contentController) DeleteReview(c *gin.Context) error {
	if err := h.review.Delete(c.Request.Context(), apictx.TenantID(c), c.Param("id")); err != nil {
		return err
	}
	response.OK(c, gin.H{"deleted": true})
	return nil
}
