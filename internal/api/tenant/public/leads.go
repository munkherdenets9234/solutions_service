package public

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// leadsController holds every write a customer of a tenant may make without
// logging in: booking requests, rental requests, airport transfers, contact
// messages, quote requests and newsletter signups.
//
// All six are mounted outside the subscription gate on purpose. A tenant
// whose subscription has lapsed still has customers trying to reach them, and
// silently dropping those requests would turn a billing problem into lost
// business the tenant never learns about.
type leadsController struct {
	booking  *service.BookingService
	rental   *service.RentalService
	transfer *service.AirportTransferService
	contact  *service.ContactMessageService
	news     *service.NewsletterService
	quote    *service.QuoteService
}

type createBookingRequest struct {
	DestinationID string          `json:"destination_id" binding:"required"`
	Customer      models.Customer `json:"customer" binding:"required"`
	Booking       models.Booking  `json:"booking" binding:"required"`
}

func (h *leadsController) CreateBooking(c *gin.Context) error {
	var req createBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return apierr.BadRequest(err.Error())
	}

	b, err := h.booking.Create(c.Request.Context(), apictx.TenantID(c), service.CreateBookingInput{
		DestinationID: req.DestinationID,
		Customer:      req.Customer,
		Booking:       req.Booking,
	})
	if err != nil {
		return err
	}
	response.Created(c, b)
	return nil
}

type createRentalRequest struct {
	CarID    string          `json:"car_id" binding:"required"`
	Customer models.Customer `json:"customer" binding:"required"`
	Rental   models.Rental   `json:"rental" binding:"required"`
}

func (h *leadsController) CreateRental(c *gin.Context) error {
	var req createRentalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return apierr.BadRequest(err.Error())
	}

	rt, err := h.rental.Create(c.Request.Context(), apictx.TenantID(c), service.CreateRentalInput{
		CarID:    req.CarID,
		Customer: req.Customer,
		Rental:   req.Rental,
	})
	if err != nil {
		return err
	}
	response.Created(c, rt)
	return nil
}

type createTransferRequest struct {
	Customer models.Customer        `json:"customer" binding:"required"`
	Transfer models.AirportTransfer `json:"transfer" binding:"required"`
}

func (h *leadsController) CreateTransfer(c *gin.Context) error {
	var req createTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return apierr.BadRequest(err.Error())
	}

	t, err := h.transfer.Create(c.Request.Context(), apictx.TenantID(c), service.CreateTransferInput{
		Customer: req.Customer,
		Transfer: req.Transfer,
	})
	if err != nil {
		return err
	}
	response.Created(c, t)
	return nil
}

func (h *leadsController) CreateContactMessage(c *gin.Context) error {
	var m models.ContactMessage
	if err := c.ShouldBindJSON(&m); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.contact.Create(c.Request.Context(), apictx.TenantID(c), &m); err != nil {
		return err
	}
	response.Created(c, m)
	return nil
}

// CreateQuote is a quote request submitted through this tenant's own
// storefront, so it is linked to that tenant. Its platform counterpart takes
// the same body with no tenant at all, for a prospect who has none yet.
func (h *leadsController) CreateQuote(c *gin.Context) error {
	var q models.Quote
	if err := c.ShouldBindJSON(&q); err != nil {
		return apierr.BadRequest(err.Error())
	}
	tid := apictx.TenantID(c)
	if err := h.quote.Create(c.Request.Context(), &tid, &q); err != nil {
		return err
	}
	response.Created(c, q)
	return nil
}

func (h *leadsController) Subscribe(c *gin.Context) error {
	var m models.NewsletterSubscriber
	if err := c.ShouldBindJSON(&m); err != nil {
		return apierr.BadRequest(err.Error())
	}
	if err := h.news.Subscribe(c.Request.Context(), apictx.TenantID(c), &m); err != nil {
		return err
	}
	response.Created(c, m)
	return nil
}
