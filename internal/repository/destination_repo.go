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

type DestinationRepo struct {
	col *mongo.Collection
}

func NewDestinationRepo(db *mongo.Database) *DestinationRepo {
	return &DestinationRepo{col: db.Collection("destinations")}
}

func (r *DestinationRepo) Create(ctx context.Context, tenantID primitive.ObjectID, d *models.Destination, userID *primitive.ObjectID) error {
	d.ID = primitive.NewObjectID()
	d.TenantID = tenantID
	d.CreatedAt = time.Now()
	d.UpdatedAt = time.Now()
	d.UserID = userID
	_, err := r.col.InsertOne(ctx, d)
	return err
}

func (r *DestinationRepo) FindAll(ctx context.Context, tenantID primitive.ObjectID, filter bson.M, page, limit int) ([]*models.Destination, int64, error) {
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

	var results []*models.Destination
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *DestinationRepo) FindBySlug(ctx context.Context, tenantID primitive.ObjectID, slug string) (*models.Destination, error) {
	var d models.Destination
	err := r.col.FindOne(ctx, bson.M{"tenant_id": tenantID, "slug": slug, "is_active": true}).Decode(&d)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DestinationRepo) FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.Destination, error) {
	var d models.Destination
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&d)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DestinationRepo) Update(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, update bson.M, userID *primitive.ObjectID) error {
	stripProtectedFields(update)
	update["updated_at"] = time.Now()
	if userID != nil {
		update["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": update})
	return err
}

func (r *DestinationRepo) AddImage(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, img models.Image) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{
		"$push": bson.M{"images": img},
		"$set":  bson.M{"updated_at": time.Now()},
	})
	return err
}

func (r *DestinationRepo) Delete(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, userID *primitive.ObjectID) error {
	set := bson.M{"is_active": false, "updated_at": time.Now()}
	if userID != nil {
		set["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": set})
	return err
}
