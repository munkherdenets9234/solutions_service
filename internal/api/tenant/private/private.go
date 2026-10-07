// Package private holds the tenant surface that requires a logged-in tenant
// user on top of the tenant's X-API-Key.
//
// Every group registered here carries an Auth.Require() with the roles that
// group permits, applied on the group rather than inside the handlers. A new
// route added to one of these groups is authenticated by construction; there
// is no per-handler check to forget.
package private

import (
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/httpx"
	"github.com/gin-gonic/gin"
)

// RoleFunc builds the auth middleware for the given roles. It is supplied by
// the caller so this package cannot choose its own authentication.
type RoleFunc func(roles ...string) gin.HandlerFunc

// Deps bundles everything the private tenant controllers need.
type Deps struct {
	Auth RoleFunc

	// Subscription gates the content and operations groups. The account
	// group is deliberately left outside it.
	Subscription gin.HandlerFunc

	AuthRateLimit gin.HandlerFunc

	Destination      *service.DestinationService
	Blog             *service.BlogService
	Car              *service.CarService
	Review           *service.ReviewService
	Partner          *service.PartnerService
	Booking          *service.BookingService
	Rental           *service.RentalService
	AirportTransfer  *service.AirportTransferService
	ContactMessage   *service.ContactMessageService
	Newsletter       *service.NewsletterService
	Quote            *service.QuoteService
	Customer         *service.CustomerService
	TenantUser       *service.TenantUserService
	SitePage         *service.SitePageService
	Upload           *service.UploadService
	GuideApplication *service.GuideApplicationService
	MailOutbox       *service.MailOutboxService
}

// Register mounts the token-authenticated tenant routes onto base, which the
// caller has already put behind TenantMiddleware.
func Register(base *gin.RouterGroup, d Deps) {
	account := &accountController{svc: d.TenantUser}
	users := &usersController{svc: d.TenantUser}
	content := &contentController{
		destination: d.Destination,
		blog:        d.Blog,
		car:         d.Car,
		review:      d.Review,
		partner:     d.Partner,
	}
	ops := &operationsController{
		booking:  d.Booking,
		rental:   d.Rental,
		transfer: d.AirportTransfer,
		contact:  d.ContactMessage,
		quote:    d.Quote,
		news:     d.Newsletter,
		customer: d.Customer,
	}
	uploads := &uploadsController{svc: d.Upload}
	translations := &translationsController{svc: d.SitePage}
	guides := &guideApplicationsController{}
	if d.GuideApplication != nil { // keep the interface truly nil otherwise
		guides.svc = d.GuideApplication
	}
	mailLog := &mailOutboxController{}
	if d.MailOutbox != nil {
		mailLog.svc = d.MailOutbox
	}

	// Self-service account routes sit OUTSIDE the subscription gate. A user
	// locked out by an expired password must still be able to change it
	// during a lapsed subscription, or the admin panel can never load and
	// the tenant cannot get to the page that would fix their billing.
	// Rate limited because it takes the current password as input.
	acct := httpx.Wrap(base.Group("/account", d.Auth(), d.AuthRateLimit))
	acct.PUT("/password", account.ChangePassword)

	scoped := base.Group("", d.Subscription)

	// Tenant user management accepts superadmin as well as the tenant's own
	// admin: a platform superadmin must be able to reset a locked-out
	// tenant admin's password without holding that tenant's credentials.
	u := httpx.Wrap(scoped.Group("/admin/users", d.Auth("admin", "superadmin"), d.AuthRateLimit))
	u.POST("", users.Create)
	u.PUT("/:id", users.Update)
	u.PUT("/:id/status", users.UpdateStatus)
	u.PUT("/:id/password", users.ResetPassword)

	admin := httpx.Wrap(scoped.Group("/admin", d.Auth("admin")))

	dest := admin.Group("/destinations")
	dest.POST("", content.CreateDestination)
	dest.PUT("/:id", content.UpdateDestination)
	dest.DELETE("/:id", content.DeleteDestination)

	blogs := admin.Group("/blogs")
	blogs.POST("", content.CreateBlog)
	blogs.PUT("/:id", content.UpdateBlog)
	blogs.POST("/:id/publish", content.PublishBlog)

	cars := admin.Group("/cars")
	cars.POST("", content.CreateCar)
	cars.PUT("/:id", content.UpdateCar)
	cars.DELETE("/:id", content.DeleteCar)

	partners := admin.Group("/partners")
	partners.POST("", content.CreatePartner)
	partners.PUT("/:id", content.UpdatePartner)
	partners.DELETE("/:id", content.DeletePartner)

	reviews := admin.Group("/reviews")
	reviews.POST("", content.CreateReview)
	reviews.PUT("/:id", content.UpdateReview)
	reviews.DELETE("/:id", content.DeleteReview)

	admin.Group("/bookings").PUT("/:id/status", ops.UpdateBookingStatus)
	admin.Group("/rentals").PUT("/:id/status", ops.UpdateRentalStatus)
	admin.Group("/airport-transfers").PUT("/:id/status", ops.UpdateTransferStatus)
	admin.Group("/contact-messages").PUT("/:id/status", ops.UpdateContactStatus)
	admin.Group("/quotes").PUT("/:id/status", ops.UpdateQuoteStatus)
	admin.DELETE("/newsletter/:id", ops.DeleteSubscriber)

	customers := admin.Group("/customers")
	customers.GET("", ops.ListCustomers)
	customers.GET("/:id", ops.GetCustomer)

	admin.POST("/uploads", uploads.Upload)

	// Applicant data is personal: bearer only, never the key-only reads.
	// /counts is registered before /:id so it is not read as an id.
	ga := admin.Group("/guide-applications")
	ga.GET("", guides.List)
	ga.GET("/counts", guides.Counts)
	ga.GET("/:id", guides.Get)
	ga.PATCH("/:id/status", guides.SetStatus)
	ga.POST("/:id/notes", guides.AddNote)
	ga.GET("/:id/files/:fileId", guides.FileLink)

	// Mail log and retry: admin only, because this group is Auth("admin") and
	// does not admit staff. Recipients are masked in the service.
	registerMailOutbox(admin, mailLog)

	tr := admin.Group("/translations")
	tr.GET("", translations.List)
	tr.GET("/:page", translations.Get)
	tr.PUT("/:page", translations.Save)
}
