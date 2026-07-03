package repository

import (
	"context"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type BookingRepo struct {
	col *mongo.Collection
}

func NewBookingRepo(db *mongo.Database) *BookingRepo {
	return &BookingRepo{col: db.Collection("bookings")}
}

func (r *BookingRepo) Create(ctx context.Context, tenantID primitive.ObjectID, b *models.Booking) error {
	b.ID = primitive.NewObjectID()
	b.TenantID = tenantID
	b.Status = models.BookingPending
	b.PaymentStatus = models.PaymentUnpaid
	b.CreatedAt = time.Now()
	b.UpdatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, b)
	return err
}

func (r *BookingRepo) FindAll(ctx context.Context, tenantID primitive.ObjectID, filter bson.M, page, limit int) ([]*models.Booking, int64, error) {
	filter["tenant_id"] = tenantID

	total, err := r.col.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().SetSkip(skip).SetLimit(int64(limit)).SetSort(bson.D{{Key: "created_at", Value: -1}})

	cur, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)

	var results []*models.Booking
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *BookingRepo) FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.Booking, error) {
	var b models.Booking
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&b)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *BookingRepo) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, status models.BookingStatus) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": bson.M{
		"status":     status,
		"updated_at": time.Now(),
	}})
	return err
}

func (r *BookingRepo) UpdatePayment(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, status models.PaymentStatus) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": bson.M{
		"payment_status": status,
		"updated_at":     time.Now(),
	}})
	return err
}
