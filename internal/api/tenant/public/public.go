// Package public holds the tenant surface reachable with the tenant's
// X-API-Key alone — no user, no bearer token.
//
// The key authenticates an *application* (a storefront), not a person. Every
// controller here therefore scopes its work to the tenant the middleware
// resolved and never reads a user identity, because on these routes there
// isn't one.
//
// Two groups are mounted, and the difference matters:
//
//   - The lead forms and login are mounted OUTSIDE the subscription gate. A
//     tenant whose subscription has lapsed must still receive bookings and
//     must still be able to log in, or a billing problem becomes a total
//     outage for their customers and locks them out of fixing it.
//   - Everything else sits behind the gate.
package public

import (
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/httpx"
	"github.com/gin-gonic/gin"
)

// Deps bundles everything the public tenant controllers need.
type Deps struct {
	Destination     *service.DestinationService
	Blog            *service.BlogService
	Car             *service.CarService
	Review          *service.ReviewService
	Partner         *service.PartnerService
	Package         *service.PackageService
	Booking         *service.BookingService
	Rental          *service.RentalService
	AirportTransfer *service.AirportTransferService
	ContactMessage  *service.ContactMessageService
	Newsletter      *service.NewsletterService
	Quote           *service.QuoteService
	TenantUser      *service.TenantUserService

	// Subscription gates the storefront reads. Supplied by the caller rather
	// than built here so this package cannot decide which of its own routes
	// are gated.
	Subscription gin.HandlerFunc

	AuthRateLimit gin.HandlerFunc
	LeadRateLimit gin.HandlerFunc
}

// Register mounts the key-authenticated tenant routes onto base, which the
// caller has already put behind TenantMiddleware.
func Register(base *gin.RouterGroup, d Deps) {
	leads := &leadsController{
		booking:  d.Booking,
		rental:   d.Rental,
		transfer: d.AirportTransfer,
		contact:  d.ContactMessage,
		news:     d.Newsletter,
		quote:    d.Quote,
	}
	auth := &authController{svc: d.TenantUser}
	store := &storefrontController{
		destination: d.Destination,
		blog:        d.Blog,
		car:         d.Car,
		review:      d.Review,
		partner:     d.Partner,
		pkg:         d.Package,
	}

	// ── Outside the subscription gate ────────────────────────────────────
	exempt := httpx.Wrap(base)

	// Anonymous writes. Rate limited because each one costs us a database
	// row and the caller proves nothing beyond holding a key that is, by
	// design, published in the storefront's own JavaScript.
	lead := exempt.Group("", d.LeadRateLimit)
	lead.POST("/bookings", leads.CreateBooking)
	lead.POST("/rentals", leads.CreateRental)
	lead.POST("/airport-transfers", leads.CreateTransfer)
	lead.POST("/contact", leads.CreateContactMessage)
	lead.POST("/quotes", leads.CreateQuote)
	lead.POST("/newsletter", leads.Subscribe)

	// Login answers differently for a known and an unknown email, which makes
	// it an account-existence oracle as well as a guessing target.
	exempt.Group("", d.AuthRateLimit).POST("/login", auth.Login)

	// ── Behind the subscription gate ─────────────────────────────────────
	// The gate only blocks mutating methods (see SubscriptionMiddleware), so
	// a lapsed tenant's storefront still reads.
	scoped := httpx.Wrap(base.Group("", d.Subscription))

	dest := scoped.Group("/destinations")
	dest.GET("", store.ListDestinations)
	dest.GET("/:slug", store.GetDestination)

	blogs := scoped.Group("/blogs")
	blogs.GET("", store.ListBlogs)
	blogs.GET("/:slug", store.GetBlog)

	cars := scoped.Group("/cars")
	cars.GET("", store.ListCars)
	cars.GET("/:slug", store.GetCar)

	partners := scoped.Group("/partners")
	partners.GET("", store.ListPartners)
	partners.GET("/:slug", store.GetPartner)

	packages := scoped.Group("/packages")
	packages.GET("", store.ListPackages)
	packages.GET("/:slug", store.GetPackage)

	reviews := scoped.Group("/reviews")
	reviews.GET("", store.ListReviews)
	// A site visitor submitting a review is a lead form by another name.
	reviews.Group("", d.LeadRateLimit).POST("", store.CreateReview)

	registerAdminReads(scoped, d)
}
