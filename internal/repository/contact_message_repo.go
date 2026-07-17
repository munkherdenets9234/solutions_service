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

type ContactMessageRepo struct {
	col *mongo.Collection
}

func NewContactMessageRepo(db *mongo.Database) *ContactMessageRepo {
	return &ContactMessageRepo{col: db.Collection("contact_messages")}
}

func (r *ContactMessageRepo) Create(ctx context.Context, tenantID primitive.ObjectID, m *models.ContactMessage) error {
	m.ID = primitive.NewObjectID()
	m.TenantID = tenantID
	m.Status = models.ContactNew
	m.CreatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, m)
	return err
}

func (r *ContactMessageRepo) FindAll(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.ContactMessage, int64, error) {
	filter := bson.M{"tenant_id": tenantID}
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

	var results []*models.ContactMessage
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *ContactMessageRepo) FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.ContactMessage, error) {
	var m models.ContactMessage
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&m)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *ContactMessageRepo) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, status models.ContactStatus, userID *primitive.ObjectID) error {
	set := bson.M{
		"status":     status,
		"updated_at": time.Now(),
	}
	if userID != nil {
		set["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": set})
	return err
}
