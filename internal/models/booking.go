package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type BookingStatus string
type PaymentStatus string

const (
	BookingPending   BookingStatus = "pending"
	BookingConfirmed BookingStatus = "confirmed"
	BookingCancelled BookingStatus = "cancelled"
	BookingCompleted BookingStatus = "completed"

	PaymentUnpaid  PaymentStatus = "unpaid"
	PaymentPartial PaymentStatus = "partial"
	PaymentPaid    PaymentStatus = "paid"
)

type Booking struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID      primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	DestinationID primitive.ObjectID `bson:"destination_id" json:"destination_id"`
	CustomerID    primitive.ObjectID `bson:"customer_id" json:"customer_id"`

	TravelDates struct {
		Start time.Time `bson:"start" json:"start"`
		End   time.Time `bson:"end" json:"end"`
	} `bson:"travel_dates" json:"travel_dates"`

	Travelers struct {
		Adults   int `bson:"adults" json:"adults"`
		Children int `bson:"children" json:"children"`
	} `bson:"travelers" json:"travelers"`

	TotalPriceUSD float64       `bson:"total_price_usd" json:"total_price_usd"`
	Status        BookingStatus `bson:"status" json:"status"`
	PaymentStatus PaymentStatus `bson:"payment_status" json:"payment_status"`
	Notes         string        `bson:"notes" json:"notes"`

	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}
