package public

import (
	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/httpx"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
)

// These are the tenant admin panel's READ endpoints. They live in the public
// package because that is the truth about how they are authenticated: they
// require the tenant's X-API-Key and nothing else — no bearer token, no user.
//
// KNOWN EXPOSURE, recorded here rather than hidden by filing them under
// "private". The X-API-Key is meant to be embedded in the tenant's own
// storefront JavaScript, so it is public by design. Anyone who reads it can
// therefore call these routes and read that tenant's bookings, rentals,
// transfers, contact messages, quotes and newsletter subscribers — which
// carry customer names, emails and phone numbers. TenantMiddleware's
// Origin/Referer domain check narrows this for browser traffic but is
// deliberately skipped when neither header is present, which is exactly the
// case for curl or Postman.
//
// Closing it is a one-line change: move registerAdminReads into
// tenant/private, where the group already carries Auth.Require(). It is not
// done here because the admin frontend does not currently send a bearer token
// on reads, and making that change without the frontend would take the panel
// down. The structure is arranged so the fix is a move, not a rewrite.
func registerAdminReads(scoped *httpx.G, d Deps) {
	r := &adminReadsController{
		tenantUser:  d.TenantUser,
		blog:        d.Blog,
		destination: d.Destination,
		partner:     d.Partner,
		review:      d.Review,
		booking:     d.Booking,
		rental:      d.Rental,
		transfer:    d.AirportTransfer,
		contact:     d.ContactMessage,
		quote:       d.Quote,
		news:        d.Newsletter,
	}

	g := scoped.Group("/admin")
	g.GET("/users", r.ListUsers)
	g.GET("/blogs", r.ListBlogs)
	g.GET("/blogs/:id", r.GetBlog)
	g.GET("/destinations", r.ListDestinations)
	g.GET("/destinations/:id", r.GetDestination)
	g.GET("/partners", r.ListPartners)
	g.GET("/partners/:id", r.GetPartner)
	g.GET("/reviews", r.ListReviews)
	g.GET("/reviews/:id", r.GetReview)
	g.GET("/bookings", r.ListBookings)
	g.GET("/bookings/:id", r.GetBooking)
	g.GET("/rentals", r.ListRentals)
	g.GET("/rentals/:id", r.GetRental)
	g.GET("/airport-transfers", r.ListTransfers)
	g.GET("/airport-transfers/:id", r.GetTransfer)
	g.GET("/contact-messages", r.ListContactMessages)
	g.GET("/quotes", r.ListQuotes)
	g.GET("/newsletter", r.ListNewsletter)
}

// adminReadsController serves the admin panel's reads. Unlike the storefront
// controller it returns the stored models with their full locale maps
// intact — the CMS edits every language at once, so resolving to one locale
// would discard the other translations before the form ever sees them.
type adminReadsController struct {
	tenantUser  *service.TenantUserService
	blog        *service.BlogService
	destination *service.DestinationService
	partner     *service.PartnerService
	review      *service.ReviewService
	booking     *service.BookingService
	rental      *service.RentalService
	transfer    *service.AirportTransferService
	contact     *service.ContactMessageService
	quote       *service.QuoteService
	news        *service.NewsletterService
}

func (h *adminReadsController) ListUsers(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	data, total, err := h.tenantUser.List(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *adminReadsController) ListBlogs(c *gin.Context) error {
	page, limit := apictx.Page(c, 10)
	status := models.BlogStatus(c.Query("status"))

	data, total, err := h.blog.ListAll(c.Request.Context(), apictx.TenantID(c), page, limit, status)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *adminReadsController) GetBlog(c *gin.Context) error {
	b, err := h.blog.GetByID(c.Request.Context(), apictx.TenantID(c), c.Param("id"))
	if err != nil {
		return err
	}
	response.OK(c, b)
	return nil
}

func (h *adminReadsController) ListDestinations(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	data, total, err := h.destination.ListAdmin(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *adminReadsController) GetDestination(c *gin.Context) error {
	d, err := h.destination.GetByID(c.Request.Context(), apictx.TenantID(c), c.Param("id"))
	if err != nil {
		return err
	}
	response.OK(c, d)
	return nil
}

func (h *adminReadsController) ListPartners(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	data, total, err := h.partner.ListAdmin(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *adminReadsController) GetPartner(c *gin.Context) error {
	p, err := h.partner.GetByID(c.Request.Context(), apictx.TenantID(c), c.Param("id"))
	if err != nil {
		return err
	}
	response.OK(c, p)
	return nil
}

func (h *adminReadsController) ListReviews(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	data, total, err := h.review.List(c.Request.Context(), apictx.TenantID(c), service.ListReviewsFilter{
		Tour:    c.Query("tour"),
		Partner: c.Query("partner"),
		Page:    page,
		Limit:   limit,
	})
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *adminReadsController) GetReview(c *gin.Context) error {
	rev, err := h.review.GetByID(c.Request.Context(), apictx.TenantID(c), c.Param("id"))
	if err != nil {
		return err
	}
	response.OK(c, rev)
	return nil
}

func (h *adminReadsController) ListBookings(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	data, total, err := h.booking.List(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *adminReadsController) GetBooking(c *gin.Context) error {
	b, err := h.booking.GetByID(c.Request.Context(), apictx.TenantID(c), c.Param("id"))
	if err != nil {
		return err
	}
	response.OK(c, b)
	return nil
}

func (h *adminReadsController) ListRentals(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	data, total, err := h.rental.List(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *adminReadsController) GetRental(c *gin.Context) error {
	rt, err := h.rental.GetByID(c.Request.Context(), apictx.TenantID(c), c.Param("id"))
	if err != nil {
		return err
	}
	response.OK(c, rt)
	return nil
}

func (h *adminReadsController) ListTransfers(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	data, total, err := h.transfer.List(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *adminReadsController) GetTransfer(c *gin.Context) error {
	t, err := h.transfer.GetByID(c.Request.Context(), apictx.TenantID(c), c.Param("id"))
	if err != nil {
		return err
	}
	response.OK(c, t)
	return nil
}

func (h *adminReadsController) ListContactMessages(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	data, total, err := h.contact.List(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *adminReadsController) ListQuotes(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	data, total, err := h.quote.List(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}

func (h *adminReadsController) ListNewsletter(c *gin.Context) error {
	page, limit := apictx.Page(c, 20)
	data, total, err := h.news.List(c.Request.Context(), apictx.TenantID(c), page, limit)
	if err != nil {
		return err
	}
	response.List(c, data, response.Meta{Total: total, Page: page, Limit: limit})
	return nil
}
