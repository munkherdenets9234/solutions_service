package handler

import (
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/gin-gonic/gin"
)

type Router struct {
	destination     *DestinationHandler
	booking         *BookingHandler
	blog            *BlogHandler
	car             *CarHandler
	rental          *RentalHandler
	airportTransfer *AirportTransferHandler
	contactMessage  *ContactMessageHandler
	tenant          *TenantHandler
	tenantUser      *TenantUserHandler
	platformUser    *PlatformUserHandler
	subscription    *SubscriptionHandler
	upload          *UploadHandler
	auth            *middleware.AuthMiddleware
	tenantMW        *middleware.TenantMiddleware
	subscriptionMW  *middleware.SubscriptionMiddleware
}

func NewRouter(
	destination *DestinationHandler,
	booking *BookingHandler,
	blog *BlogHandler,
	car *CarHandler,
	rental *RentalHandler,
	airportTransfer *AirportTransferHandler,
	contactMessage *ContactMessageHandler,
	tenant *TenantHandler,
	tenantUser *TenantUserHandler,
	platformUser *PlatformUserHandler,
	subscription *SubscriptionHandler,
	upload *UploadHandler,
	auth *middleware.AuthMiddleware,
	tenantMW *middleware.TenantMiddleware,
	subscriptionMW *middleware.SubscriptionMiddleware,
) *Router {
	return &Router{
		destination:     destination,
		booking:         booking,
		blog:            blog,
		car:             car,
		rental:          rental,
		airportTransfer: airportTransfer,
		contactMessage:  contactMessage,
		tenant:          tenant,
		tenantUser:      tenantUser,
		platformUser:    platformUser,
		subscription:    subscription,
		upload:          upload,
		auth:            auth,
		tenantMW:        tenantMW,
		subscriptionMW:  subscriptionMW,
	}
}

func (r *Router) Register(engine *gin.Engine) {
	engine.Use(middleware.Logger())
	engine.Use(middleware.CORS())

	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Swagger UI API reference. Not tenant-scoped — no X-API-Key required,
	// it just describes the routes below.
	engine.GET("/docs", func(c *gin.Context) {
		c.Data(200, "text/html; charset=utf-8", DocsHTML)
	})
	engine.GET("/docs/openapi.json", func(c *gin.Context) {
		c.Data(200, "application/json; charset=utf-8", OpenAPISpec)
	})

	api := engine.Group("/api/v1")

	// Platform routes — manage tenants themselves. Not tenant-scoped: no
	// X-API-Key required. Login and all GET reads are public; every
	// mutating request is gated on the "superadmin" role.
	platform := api.Group("/platform")
	{
		platform.POST("/login", r.platformUser.Login) // public — platform user login, issues a superadmin token

		// Public reads — no auth required.
		platform.GET("/tenants", r.tenant.List)
		platform.GET("/tenants/:id", r.tenant.GetByID)
		platform.GET("/tenants/:id/subscription", r.subscription.Get)
		platform.GET("/admins", r.platformUser.List)

		platformAuthed := platform.Group("")
		platformAuthed.Use(r.auth.Require("superadmin"))
		{
			platformTenants := platformAuthed.Group("/tenants")
			platformTenants.POST("", r.tenant.Create)
			platformTenants.PUT("/:id/status", r.tenant.UpdateStatus)
			platformTenants.POST("/:id/rotate-key", r.tenant.RotateAPIKey)
			platformTenants.PUT("/:id/domain", r.tenant.UpdateDomain)
			platformTenants.POST("/:id/subscription", r.subscription.Create)
			platformTenants.PUT("/:id/subscription/plan", r.subscription.UpdatePlan)
			platformTenants.POST("/:id/subscription/cancel", r.subscription.Cancel)

			platformAdmins := platformAuthed.Group("/admins")
			platformAdmins.POST("", r.platformUser.Create)
			platformAdmins.PUT("/:id/status", r.platformUser.UpdateStatus)
			platformAdmins.PUT("/:id/password", r.platformUser.ResetPassword)

			platformAuthed.PUT("/account/password", r.platformUser.ChangePassword)
		}
	}

	// Every route below requires X-API-Key and is scoped to that tenant.
	tenantBase := api.Group("")
	tenantBase.Use(r.tenantMW.Require())

	// Customer-facing lead forms — exempt from SubscriptionMiddleware so a
	// tenant's lapsed/expired subscription never blocks an incoming
	// booking, rental, transfer, or contact request from a customer.
	// Login and self-service password change are exempt too — a tenant user
	// must always be able to log in and manage their own account even during
	// a lapsed subscription, or the admin frontend can never even load.
	{
		bookings := tenantBase.Group("/bookings")
		bookings.POST("", r.booking.Create)

		rentals := tenantBase.Group("/rentals")
		rentals.POST("", r.rental.Create)

		airportTransfers := tenantBase.Group("/airport-transfers")
		airportTransfers.POST("", r.airportTransfer.Create)

		contact := tenantBase.Group("/contact")
		contact.POST("", r.contactMessage.Create)

		tenantBase.POST("/login", r.tenantUser.Login) // public — tenant user login, issues a bearer token

		// Self-service account routes (require a valid token scoped to this
		// tenant, any role — admin or staff).
		account := tenantBase.Group("/account")
		account.Use(r.auth.Require())
		account.PUT("/password", r.tenantUser.ChangePassword)
	}

	tenantScoped := tenantBase.Group("")
	tenantScoped.Use(r.subscriptionMW.Require())
	{
		// Public routes (tenant-scoped, no user auth)
		dest := tenantScoped.Group("/destinations")
		{
			dest.GET("", r.destination.List)
			dest.GET("/:slug", r.destination.GetBySlug)
		}

		blogs := tenantScoped.Group("/blogs")
		{
			blogs.GET("", r.blog.List)
			blogs.GET("/:slug", r.blog.GetBySlug)
		}

		cars := tenantScoped.Group("/cars")
		{
			cars.GET("", r.car.List)
			cars.GET("/:slug", r.car.GetBySlug)
		}

		// Admin reads — X-API-Key required, no admin bearer token needed.
		tenantScoped.GET("/admin/users", r.tenantUser.List)
		tenantScoped.GET("/admin/bookings", r.booking.List)
		tenantScoped.GET("/admin/bookings/:id", r.booking.GetByID)
		tenantScoped.GET("/admin/rentals", r.rental.List)
		tenantScoped.GET("/admin/rentals/:id", r.rental.GetByID)
		tenantScoped.GET("/admin/airport-transfers", r.airportTransfer.List)
		tenantScoped.GET("/admin/airport-transfers/:id", r.airportTransfer.GetByID)
		tenantScoped.GET("/admin/contact-messages", r.contactMessage.List)

		// Tenant user management — a platform superadmin can administer any
		// tenant's admin/staff accounts (e.g. resetting a locked-out admin's
		// password) without holding that tenant's own admin credentials, so
		// this accepts either role rather than "admin" only.
		adminUsers := tenantScoped.Group("/admin/users")
		adminUsers.Use(r.auth.Require("admin", "superadmin"))
		{
			adminUsers.POST("", r.tenantUser.Create)
			adminUsers.PUT("/:id/status", r.tenantUser.UpdateStatus)
			adminUsers.PUT("/:id/password", r.tenantUser.ResetPassword)
		}

		// Admin writes (require an "admin" token scoped to this tenant)
		admin := tenantScoped.Group("/admin")
		admin.Use(r.auth.Require("admin"))
		{
			adminDest := admin.Group("/destinations")
			adminDest.POST("", r.destination.Create)
			adminDest.PUT("/:id", r.destination.Update)
			adminDest.DELETE("/:id", r.destination.Delete)

			adminBlog := admin.Group("/blogs")
			adminBlog.POST("", r.blog.Create)
			adminBlog.PUT("/:id", r.blog.Update)
			adminBlog.POST("/:id/publish", r.blog.Publish)

			adminBooking := admin.Group("/bookings")
			adminBooking.PUT("/:id/status", r.booking.UpdateStatus)

			adminCar := admin.Group("/cars")
			adminCar.POST("", r.car.Create)
			adminCar.PUT("/:id", r.car.Update)
			adminCar.DELETE("/:id", r.car.Delete)

			adminRental := admin.Group("/rentals")
			adminRental.PUT("/:id/status", r.rental.UpdateStatus)

			adminTransfer := admin.Group("/airport-transfers")
			adminTransfer.PUT("/:id/status", r.airportTransfer.UpdateStatus)

			adminContact := admin.Group("/contact-messages")
			adminContact.PUT("/:id/status", r.contactMessage.UpdateStatus)

			admin.POST("/uploads", r.upload.Upload)
		}
	}
}
