package private

import (
	"errors"
	"io"
	"net/http"

	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// operationsController is what a tenant admin does with the leads their
// storefront collected: move them through their status workflow, and read the
// customer records they produced.
type operationsController struct {
	booking  *service.BookingService
	rental   *service.RentalService
	transfer *service.AirportTransferService
	contact  *service.ContactMessageService
	quote    *service.QuoteService
	news     *service.NewsletterService
	customer *service.CustomerService
}

func (h *operationsController) UpdateBookingStatus(c *gin.Context) error {
	var body struct {
		Status models.BookingStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.booking.UpdateStatus(c.Request.Context(), apictx.TenantID(c), c.Param("id"), body.Status, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *operationsController) UpdateRentalStatus(c *gin.Context) error {
	var body struct {
		Status models.RentalStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.rental.UpdateStatus(c.Request.Context(), apictx.TenantID(c), c.Param("id"), body.Status, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *operationsController) UpdateTransferStatus(c *gin.Context) error {
	var body struct {
		Status models.TransferStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.transfer.UpdateStatus(c.Request.Context(), apictx.TenantID(c), c.Param("id"), body.Status, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *operationsController) UpdateContactStatus(c *gin.Context) error {
	var body struct {
		Status models.ContactStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.contact.UpdateStatus(c.Request.Context(), apictx.TenantID(c), c.Param("id"), body.Status, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

// UpdateQuoteStatus is scoped to leads that actually belong to this tenant.
// The platform's counterpart can touch any quote, including tenant-less ones;
// this one cannot, and that difference is the reason there are two.
func (h *operationsController) UpdateQuoteStatus(c *gin.Context) error {
	var body struct {
		Status models.QuoteStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.quote.UpdateStatusForTenant(c.Request.Context(), apictx.TenantID(c), c.Param("id"), body.Status, apictx.ActorID(c)); err != nil {
		return err
	}
	response.OK(c, gin.H{"updated": true})
	return nil
}

func (h *operationsController) DeleteSubscriber(c *gin.Context) error {
	if err := h.news.Delete(c.Request.Context(), apictx.TenantID(c), c.Param("id")); err != nil {
		return err
	}
	response.OK(c, gin.H{"deleted": true})
	return nil
}

func (h *operationsController) ListCustomers(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)

	data, total, err := h.customer.List(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

// maxCustomerFormBytes bounds the multipart body of POST /admin/customers:
// the avatar cap plus headroom for the text fields. UploadService enforces
// its own, tighter per-file cap.
const maxCustomerFormBytes = 12 << 20

// CreateCustomer creates a customer from a multipart admin form. Fields:
// name, email, phone, nationality, and an optional image file field "avatar".
func (h *operationsController) CreateCustomer(c *gin.Context) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCustomerFormBytes)

	cust := &models.Customer{
		Name:        c.PostForm("name"),
		Email:       c.PostForm("email"),
		Phone:       c.PostForm("phone"),
		Nationality: c.PostForm("nationality"),
	}

	var avatar io.Reader
	fh, err := c.FormFile("avatar")
	switch {
	case err == nil:
		f, oerr := fh.Open()
		if oerr != nil {
			return apierr.BadRequest("could not read avatar")
		}
		defer f.Close()
		avatar = f
	case errors.Is(err, http.ErrMissingFile):
		// no avatar: fine
	default:
		return apierr.BadRequest("invalid form")
	}

	created, err := h.customer.CreateManual(c.Request.Context(), apictx.TenantID(c), cust, avatar, apictx.ActorID(c))
	if err != nil {
		return err
	}
	response.Created(c, created)
	return nil
}

func (h *operationsController) GetCustomer(c *gin.Context) error {
	data, err := h.customer.GetByID(c.Request.Context(), apictx.TenantID(c), c.Param("id"))
	if err != nil {
		return err
	}
	response.OK(c, data)
	return nil
}
