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
	auth            *middleware.AuthMiddleware
	tenantMW        *middleware.TenantMiddleware
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
	auth *middleware.AuthMiddleware,
	tenantMW *middleware.TenantMiddleware,
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
		auth:            auth,
		tenantMW:        tenantMW,
	}
}

func (r *Router) Register(engine *gin.Engine) {
	engine.Use(middleware.Logger())
	engine.Use(middleware.CORS())

	api := engine.Group("/api/v1")

	// Platform routes — manage tenants themselves. Not tenant-scoped: no
	// X-API-Key required. Login is public; everything else is gated on the
	// "superadmin" role.
	platform := api.Group("/platform")
	{
		platform.POST("/login", r.platformUser.Login) // public — platform user login, issues a superadmin token

		platformAuthed := platform.Group("")
		platformAuthed.Use(r.auth.Require("superadmin"))
		{
			platformTenants := platformAuthed.Group("/tenants")
			platformTenants.POST("", r.tenant.Create)
			platformTenants.GET("", r.tenant.List)
			platformTenants.GET("/:id", r.tenant.GetByID)
			platformTenants.PUT("/:id/status", r.tenant.UpdateStatus)
			platformTenants.POST("/:id/rotate-key", r.tenant.RotateAPIKey)

			platformAdmins := platformAuthed.Group("/admins")
			platformAdmins.POST("", r.platformUser.Create)
			platformAdmins.GET("", r.platformUser.List)
			platformAdmins.PUT("/:id/status", r.platformUser.UpdateStatus)
		}
	}

	// Every route below requires X-API-Key and is scoped to that tenant.
	tenantScoped := api.Group("")
	tenantScoped.Use(r.tenantMW.Require())
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

		bookings := tenantScoped.Group("/bookings")
		{
			bookings.POST("", r.booking.Create) // public — customers create bookings
		}

		cars := tenantScoped.Group("/cars")
		{
			cars.GET("", r.car.List)
			cars.GET("/:slug", r.car.GetBySlug)
		}

		rentals := tenantScoped.Group("/rentals")
		{
			rentals.POST("", r.rental.Create) // public — customers create rentals
		}

		airportTransfers := tenantScoped.Group("/airport-transfers")
		{
			airportTransfers.POST("", r.airportTransfer.Create) // public — customers request transfers
		}

		contact := tenantScoped.Group("/contact")
		{
			contact.POST("", r.contactMessage.Create) // public — contact form submissions
		}

		tenantScoped.POST("/login", r.tenantUser.Login) // public — tenant user login, issues a bearer token

		// Admin routes (require an "admin" token scoped to this tenant)
		admin := tenantScoped.Group("/admin")
		admin.Use(r.auth.Require("admin"))
		{
			adminUsers := admin.Group("/users")
			adminUsers.POST("", r.tenantUser.Create)
			adminUsers.GET("", r.tenantUser.List)
			adminUsers.PUT("/:id/status", r.tenantUser.UpdateStatus)

			adminDest := admin.Group("/destinations")
			adminDest.POST("", r.destination.Create)
			adminDest.PUT("/:id", r.destination.Update)
			adminDest.DELETE("/:id", r.destination.Delete)

			adminBlog := admin.Group("/blogs")
			adminBlog.POST("", r.blog.Create)
			adminBlog.PUT("/:id", r.blog.Update)
			adminBlog.POST("/:id/publish", r.blog.Publish)

			adminBooking := admin.Group("/bookings")
			adminBooking.GET("", r.booking.List)
			adminBooking.GET("/:id", r.booking.GetByID)
			adminBooking.PUT("/:id/status", r.booking.UpdateStatus)

			adminCar := admin.Group("/cars")
			adminCar.POST("", r.car.Create)
			adminCar.PUT("/:id", r.car.Update)
			adminCar.DELETE("/:id", r.car.Delete)

			adminRental := admin.Group("/rentals")
			adminRental.GET("", r.rental.List)
			adminRental.GET("/:id", r.rental.GetByID)
			adminRental.PUT("/:id/status", r.rental.UpdateStatus)

			adminTransfer := admin.Group("/airport-transfers")
			adminTransfer.GET("", r.airportTransfer.List)
			adminTransfer.GET("/:id", r.airportTransfer.GetByID)
			adminTransfer.PUT("/:id/status", r.airportTransfer.UpdateStatus)

			adminContact := admin.Group("/contact-messages")
			adminContact.GET("", r.contactMessage.List)
			adminContact.PUT("/:id/status", r.contactMessage.UpdateStatus)
		}
	}
}
