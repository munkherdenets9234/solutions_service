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

type PartnerRepo struct {
	col *mongo.Collection
}

func NewPartnerRepo(db *mongo.Database) *PartnerRepo {
	return &PartnerRepo{col: db.Collection("partners")}
}

func (r *PartnerRepo) Create(ctx context.Context, tenantID primitive.ObjectID, p *models.Partner) error {
	p.ID = primitive.NewObjectID()
	p.TenantID = tenantID
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, p)
	return err
}

func (r *PartnerRepo) FindAll(ctx context.Context, tenantID primitive.ObjectID, filter bson.M, page, limit int) ([]*models.Partner, int64, error) {
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

	var results []*models.Partner
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *PartnerRepo) FindBySlug(ctx context.Context, tenantID primitive.ObjectID, slug string) (*models.Partner, error) {
	var p models.Partner
	err := r.col.FindOne(ctx, bson.M{"tenant_id": tenantID, "slug": slug, "is_active": true}).Decode(&p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PartnerRepo) FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.Partner, error) {
	var p models.Partner
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PartnerRepo) Update(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, update bson.M) error {
	stripProtectedFields(update)
	update["updated_at"] = time.Now()
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": update})
	return err
}

func (r *PartnerRepo) Delete(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": bson.M{"is_active": false, "updated_at": time.Now()}})
	return err
}
