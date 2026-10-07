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
	PasswordReset   *service.TenantPasswordResetService
	SitePage        *service.SitePageService

	// SubscriptionStatus backs GET /subscription-status. Nil is allowed and
	// answers active.
	SubscriptionStatus *service.SubscriptionStatusService

	// GuideApplication and GuideUploadMaxBytes feed the public guide
	// application form. GuideUploadMaxBytes is the per-file ceiling; the body
	// ceiling is derived from it.
	GuideApplication    *service.GuideApplicationService
	GuideUploadMaxBytes int64

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
	reset := &passwordResetController{svc: d.PasswordReset}
	translations := &translationsController{svc: d.SitePage}
	subStatus := &subscriptionStatusController{svc: d.SubscriptionStatus}
	guide := &guideApplicationsController{maxBytes: d.GuideUploadMaxBytes}
	// Assign only a non-nil service: a nil *GuideApplicationService stored in
	// the interface would be a non-nil interface that panics when called.
	if d.GuideApplication != nil {
		guide.svc = d.GuideApplication
	}
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
	lead.POST("/guide-applications", guide.Submit)

	// Login answers differently for a known and an unknown email, which makes
	// it an account-existence oracle as well as a guessing target.
	authLimited := exempt.Group("", d.AuthRateLimit)
	authLimited.POST("/login", auth.Login)

	// Password reset sits beside login, OUTSIDE the subscription gate, for the
	// same reason login does: the gate blocks every mutating method, and a
	// tenant whose subscription has lapsed still needs its admins to be able to
	// get back in to see their data. Both are rate limited together, the second
	// being a guess against a six-digit code and the first costing a mail.
	authLimited.POST("/password-reset/request", reset.Request)
	authLimited.POST("/password-reset/confirm", reset.Confirm)

	// Subscription status sits beside login, OUTSIDE the gate: a lapsed tenant
	// is exactly who needs to read it, and it is a GET that reveals one word.
	// No extra limiter: the tenant-key gate in front of this group already
	// carries the resolve limiter, and the shared login bucket (AuthRateLimit)
	// would let a dashboard that polls this lock admins out of login.
	exempt.GET("/subscription-status", subStatus.Get)

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

	scoped.GET("/translations", translations.Get)

	reviews := scoped.Group("/reviews")
	reviews.GET("", store.ListReviews)
	// A site visitor submitting a review is a lead form by another name.
	reviews.Group("", d.LeadRateLimit).POST("", store.CreateReview)

	registerAdminReads(scoped, d)
}
