package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type SubscriptionStatus string

const (
	SubscriptionActive   SubscriptionStatus = "active"
	SubscriptionPastDue  SubscriptionStatus = "past_due"
	SubscriptionCanceled SubscriptionStatus = "canceled"
	SubscriptionTrialing SubscriptionStatus = "trialing"
)

// Subscription tracks which Package a tenant is on and its billing state.
// PackageID references a Package document — typically one belonging to the
// platform operator's own tenant (their own price list) — rather than a
// generic plan name, so a subscription always carries real pricing/feature
// content instead of a disconnected label. There is no live payment provider
// wired in yet — the package and status are set directly through this API by
// a platform superadmin.
type Subscription struct {
	ID                 primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID           primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	PackageID          primitive.ObjectID `bson:"package_id" json:"package_id"`
	Status             SubscriptionStatus `bson:"status" json:"status"`
	CurrentPeriodStart time.Time          `bson:"current_period_start" json:"current_period_start"`
	CurrentPeriodEnd   time.Time          `bson:"current_period_end" json:"current_period_end"`
	CanceledAt         *time.Time         `bson:"canceled_at,omitempty" json:"canceled_at,omitempty"`
	CreatedAt          time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt          time.Time          `bson:"updated_at" json:"updated_at"`
	// UserID is the platform_users._id of the superadmin who last
	// created/updated this subscription — unlike every other entity's UserID,
	// this references platform_users, not tenant_users, since subscriptions
	// are only ever managed via /platform routes. Nil if never touched.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	// LastEditedBy is UserID resolved to a display name, populated by the
	// service layer on read — not persisted.
	LastEditedBy *string `bson:"-" json:"lastEditedBy,omitempty"`
	// Package is the referenced package, resolved by the service layer on
	// read — not persisted. Left nil if the package was since deleted.
	Package *Package `bson:"-" json:"package,omitempty"`
}
