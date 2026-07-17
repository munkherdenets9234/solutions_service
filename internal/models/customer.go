package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Customer struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID    primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	Name        string             `bson:"name" json:"name"`
	Email       string             `bson:"email" json:"email"`
	Phone       string             `bson:"phone" json:"phone"`
	Nationality string             `bson:"nationality" json:"nationality"`
	Notes       string             `bson:"notes" json:"notes"`
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at" json:"updated_at"`
	// UserID is the tenant_users._id of whoever last created/updated this
	// record via the admin panel. Customers are normally upserted from public
	// booking/rental forms with no authenticated tenant user, so this is nil
	// unless a future admin-facing write path sets it.
	UserID *primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	// LastEditedBy is UserID resolved to a display name, populated by the
	// service layer on read — not persisted.
	LastEditedBy *string `bson:"-" json:"lastEditedBy,omitempty"`
}
