package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	CarModeWithDriver = "with_driver"
	CarModeSelfDrive  = "self_drive"
)

type Car struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID       primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	Slug           string             `bson:"slug" json:"slug"`
	Name           string             `bson:"name" json:"name"`
	Type           string             `bson:"type" json:"type"` // sedan | suv | van | 4x4
	Seats          int                `bson:"seats" json:"seats"`
	Fuel           string             `bson:"fuel" json:"fuel"` // petrol | diesel | hybrid | electric
	PricePerDayUSD float64            `bson:"price_per_day_usd" json:"price_per_day_usd"`
	Tags           []string           `bson:"tags" json:"tags"`
	CoverImage     Image              `bson:"cover_image" json:"cover_image"`
	Images         []Image            `bson:"images" json:"images"`
	IsActive       bool               `bson:"is_active" json:"is_active"`
	// IsVisible is a pointer so a stored-absent value (legacy car, treated as
	// visible) differs from an explicit false.
	IsVisible *bool `bson:"is_visible" json:"is_visible"`
	// RentalModes lists the modes offered; empty means both.
	RentalModes []string `bson:"rental_modes" json:"rental_modes"`
	// SelfDriveFrom/To bound the self-drive rental window (date-only UTC,
	// inclusive). Both nil means no limit. No omitempty so null round-trips.
	SelfDriveFrom *time.Time `bson:"self_drive_from" json:"self_drive_from"`
	SelfDriveTo   *time.Time `bson:"self_drive_to" json:"self_drive_to"`
	CreatedAt     time.Time  `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `bson:"updated_at" json:"updated_at"`
	// UserID is the tenant_users._id of whoever last created/updated this
	// record via the admin panel. Nil if never touched by an authenticated
	// tenant user.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	// LastEditedBy is UserID resolved to a display name, populated by the
	// service layer on read — not persisted.
	LastEditedBy *string `bson:"-" json:"lastEditedBy,omitempty"`
}
