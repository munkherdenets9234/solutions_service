package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type TenantStatus string

const (
	TenantActive    TenantStatus = "active"
	TenantSuspended TenantStatus = "suspended"
)

type Tenant struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name         string             `bson:"name" json:"name"`
	Slug         string             `bson:"slug" json:"slug"`
	ContactEmail string             `bson:"contact_email" json:"contact_email"`
	APIKeyHash   string             `bson:"api_key_hash" json:"-"`
	APIKeyLast4  string             `bson:"api_key_last4" json:"api_key_last4"`
	Status       TenantStatus       `bson:"status" json:"status"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
}
