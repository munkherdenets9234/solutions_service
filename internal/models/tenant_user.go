package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type TenantUserRole string

const (
	TenantUserAdmin TenantUserRole = "admin"
	TenantUserStaff TenantUserRole = "staff"
)

type TenantUserStatus string

const (
	TenantUserActive    TenantUserStatus = "active"
	TenantUserSuspended TenantUserStatus = "suspended"
)

// TenantUser is a login profile scoped to a single tenant. A tenant can have
// several of these (e.g. one admin, several staff) — unlike the tenant's
// single API key, which authenticates the tenant's application, these
// authenticate individual people logging into the admin panel.
type TenantUser struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID     primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	Name         string             `bson:"name" json:"name"`
	Email        string             `bson:"email" json:"email"`
	PasswordHash string             `bson:"password_hash" json:"-"`
	Role         TenantUserRole     `bson:"role" json:"role"`
	Status       TenantUserStatus   `bson:"status" json:"status"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
}
