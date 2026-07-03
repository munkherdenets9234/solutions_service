package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type PlatformUserStatus string

const (
	PlatformUserActive    PlatformUserStatus = "active"
	PlatformUserSuspended PlatformUserStatus = "suspended"
)

// PlatformUser is a login profile for the platform team — not tenant-scoped.
// Every PlatformUser is a superadmin; there is only one role at this level
// today, unlike TenantUser which distinguishes admin/staff.
type PlatformUser struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name         string             `bson:"name" json:"name"`
	Email        string             `bson:"email" json:"email"`
	PasswordHash string             `bson:"password_hash" json:"-"`
	Status       PlatformUserStatus `bson:"status" json:"status"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
}
