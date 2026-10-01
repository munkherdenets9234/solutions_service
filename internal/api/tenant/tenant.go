// Package tenant is the audience scoped to one tenant by its X-API-Key: the
// storefront its customers see, and the admin panel its staff use.
//
// Every route below TenantMiddleware is scoped to the resolved tenant, so no
// controller here can read or write another tenant's data even with a valid
// id in the path.
package tenant

import (
	privateapi "github.com/eandstravel/digitalservice/internal/api/tenant/private"
	publicapi "github.com/eandstravel/digitalservice/internal/api/tenant/public"
	"github.com/eandstravel/digitalservice/internal/entitlement"
	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/gin-gonic/gin"
)

// Module is one product mounted into this service.
//
// This is the seam the "one deployment per product" split runs through.
// Today a module is registered in-process and mounted at /api/v1/<name>;
// the day it moves to its own deployment, what changes is where Register
// lives — not how it is gated, not what it is allowed to assume about the
// tenant, and not the URL its clients call.
//
// Register receives a group that already carries, in order: the X-API-Key
// tenant resolution, the subscription gate, and RequireModule(Name). A
// module cannot opt out of any of them, which is the point: a product is
// entitlement-checked by construction, exactly as the private groups are
// auth-checked by construction.
type Module struct {
	// Name is both the URL segment and the entitlement key. One string, so a
	// plan that grants "carwash" and a route tree mounted at /carwash cannot
	// drift apart.
	Name     string
	Register func(g *gin.RouterGroup)
}

// Deps bundles everything the tenant audience needs.
type Deps struct {
	Auth         *middleware.AuthMiddleware
	Tenant       *middleware.TenantMiddleware
	Subscription *middleware.SubscriptionMiddleware

	// Entitlement answers what the tenant's plan allows. Required only when
	// Modules is non-empty.
	Entitlement entitlement.Provider

	// Modules are the additional products this deployment serves. The
	// brochure/travel surface below is not one of them yet — it predates
	// modules and is mounted directly, so existing tenants are unaffected.
	// Moving it behind RequireModule("travel") is a one-line change, and the
	// right moment is when every plan lists its modules (see
	// entitlement.HasModule).
	Modules []Module

	AuthRateLimit gin.HandlerFunc
	LeadRateLimit gin.HandlerFunc

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
	Customer        *service.CustomerService
	TenantUser      *service.TenantUserService
	PasswordReset   *service.TenantPasswordResetService
	Upload          *service.UploadService
}

// Register mounts the tenant audience onto base.
//
// The X-API-Key check is applied once, here, on the group both sub-packages
// hang off. Neither of them can opt out of it.
func Register(base *gin.RouterGroup, d Deps) {
	scoped := base.Group("", d.Tenant.Require())
	subscription := d.Subscription.Require()

	publicapi.Register(scoped, publicapi.Deps{
		Destination:     d.Destination,
		Blog:            d.Blog,
		Car:             d.Car,
		Review:          d.Review,
		Partner:         d.Partner,
		Package:         d.Package,
		Booking:         d.Booking,
		Rental:          d.Rental,
		AirportTransfer: d.AirportTransfer,
		ContactMessage:  d.ContactMessage,
		Newsletter:      d.Newsletter,
		Quote:           d.Quote,
		TenantUser:      d.TenantUser,
		PasswordReset:   d.PasswordReset,
		Subscription:    subscription,
		AuthRateLimit:   d.AuthRateLimit,
		LeadRateLimit:   d.LeadRateLimit,
	})

	// Modules first: each gets its own prefix, so a product's route names
	// cannot collide with the brochure surface below or with each other.
	// That is not hypothetical — /cars already means "rental fleet vehicle"
	// here and "the customer's own car" in the car wash service.
	for _, m := range d.Modules {
		g := scoped.Group("/"+m.Name, subscription, middleware.RequireModule(d.Entitlement, m.Name))
		m.Register(g)
	}

	privateapi.Register(scoped, privateapi.Deps{
		Auth:            d.Auth.Require,
		Subscription:    subscription,
		AuthRateLimit:   d.AuthRateLimit,
		Destination:     d.Destination,
		Blog:            d.Blog,
		Car:             d.Car,
		Review:          d.Review,
		Partner:         d.Partner,
		Booking:         d.Booking,
		Rental:          d.Rental,
		AirportTransfer: d.AirportTransfer,
		ContactMessage:  d.ContactMessage,
		Newsletter:      d.Newsletter,
		Quote:           d.Quote,
		Customer:        d.Customer,
		TenantUser:      d.TenantUser,
		Upload:          d.Upload,
	})
}
