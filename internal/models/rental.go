package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type RentalMode string
type RentalStatus string

const (
	RentalSelfDrive  RentalMode = "self_drive"
	RentalWithDriver RentalMode = "with_driver"

	RentalPending   RentalStatus = "pending"
	RentalConfirmed RentalStatus = "confirmed"
	RentalCancelled RentalStatus = "cancelled"
	RentalCompleted RentalStatus = "completed"
)

type Rental struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID       primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	CarID          primitive.ObjectID `bson:"car_id" json:"car_id"`
	CustomerID     primitive.ObjectID `bson:"customer_id" json:"customer_id"`
	Mode           RentalMode         `bson:"mode" json:"mode"`
	PickupDate     time.Time          `bson:"pickup_date" json:"pickup_date"`
	ReturnDate     time.Time          `bson:"return_date" json:"return_date"`
	Notes          string             `bson:"notes" json:"notes"`
	Status         RentalStatus       `bson:"status" json:"status"`
	ConfirmationID string             `bson:"confirmation_id" json:"confirmation_id"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updated_at"`
}
