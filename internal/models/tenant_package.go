package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TenantPackage maps a tenant to one of the platform's global Package
// catalog entries — which packages a given tenant's own storefront shows.
// Packages themselves are managed platform-wide (see Package); this is the
// many-to-many assignment between tenants and that shared catalog, managed
// only via /platform routes.
type TenantPackage struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID  primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	PackageID primitive.ObjectID `bson:"package_id" json:"package_id"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	// UserID is the platform_users._id of the superadmin who created this
	// assignment. Nil if never touched.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
}
