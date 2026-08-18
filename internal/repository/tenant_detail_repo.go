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

type TenantDetailRepo struct {
	col *mongo.Collection
}

func NewTenantDetailRepo(db *mongo.Database) *TenantDetailRepo {
	return &TenantDetailRepo{col: db.Collection("tenant_details")}
}

// Upsert creates or updates a tenant's project-showcase detail record —
// there's no separate explicit "create" step exposed to the API, since a
// tenant detail is a 1:1 extension of its tenant rather than an independent
// resource. update is the raw client-supplied $set map (partial update,
// same pattern as Partner/Package), so protected fields are stripped here
// rather than trusted from the caller.
func (r *TenantDetailRepo) Upsert(ctx context.Context, tenantID primitive.ObjectID, update bson.M, userID *primitive.ObjectID) error {
	stripProtectedFields(update)
	update["updated_at"] = time.Now()
	if userID != nil {
		update["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx,
		bson.M{"tenant_id": tenantID},
		bson.M{
			"$set":         update,
			"$setOnInsert": bson.M{"tenant_id": tenantID, "created_at": time.Now()},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

func (r *TenantDetailRepo) FindByTenantID(ctx context.Context, tenantID primitive.ObjectID) (*models.TenantDetail, error) {
	var d models.TenantDetail
	err := r.col.FindOne(ctx, bson.M{"tenant_id": tenantID}).Decode(&d)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// FindByTenantIDs batch-loads details for the given tenants, for embedding
// into GET /platform/tenants (list). Tenants with no detail record are
// simply absent from the result — not an error.
func (r *TenantDetailRepo) FindByTenantIDs(ctx context.Context, tenantIDs []primitive.ObjectID) ([]*models.TenantDetail, error) {
	if len(tenantIDs) == 0 {
		return nil, nil
	}
	cur, err := r.col.Find(ctx, bson.M{"tenant_id": bson.M{"$in": tenantIDs}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var results []*models.TenantDetail
	if err := cur.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// FindAllShowcase returns tenant_details marked Showcase=true, sorted by
// sort_order — the "Our Projects" public listing's detail half. TenantService
// joins these against their active tenants.
func (r *TenantDetailRepo) FindAllShowcase(ctx context.Context, page, limit int) ([]*models.TenantDetail, int64, error) {
	filter := bson.M{"showcase": true}

	total, err := r.col.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().SetSkip(skip).SetLimit(int64(limit)).
		SetSort(bson.D{{Key: "sort_order", Value: 1}, {Key: "created_at", Value: -1}})

	cur, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)

	var results []*models.TenantDetail
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}
