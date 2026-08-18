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

type PackageRepo struct {
	col *mongo.Collection
}

func NewPackageRepo(db *mongo.Database) *PackageRepo {
	return &PackageRepo{col: db.Collection("packages")}
}

func (r *PackageRepo) Create(ctx context.Context, p *models.Package, userID *primitive.ObjectID) error {
	p.ID = primitive.NewObjectID()
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	p.UserID = userID
	_, err := r.col.InsertOne(ctx, p)
	return err
}

// FindAll sorts by sort_order ascending (the platform-admin-controlled
// display order), then created_at as a stable tiebreak for packages sharing
// a sort_order.
func (r *PackageRepo) FindAll(ctx context.Context, filter bson.M, page, limit int) ([]*models.Package, int64, error) {
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

	var results []*models.Package
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *PackageRepo) FindBySlug(ctx context.Context, slug string) (*models.Package, error) {
	var p models.Package
	err := r.col.FindOne(ctx, bson.M{"slug": slug, "is_active": true}).Decode(&p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PackageRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*models.Package, error) {
	var p models.Package
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// FindByIDsActive returns active packages among the given ids, paginated —
// the tenant-scoped storefront read, joined against TenantPackage by the
// service layer.
func (r *PackageRepo) FindByIDsActive(ctx context.Context, ids []primitive.ObjectID, page, limit int) ([]*models.Package, int64, error) {
	if len(ids) == 0 {
		return nil, 0, nil
	}
	return r.FindAll(ctx, bson.M{"_id": bson.M{"$in": ids}, "is_active": true}, page, limit)
}

// FindByIDs returns every package among the given ids regardless of active
// status, paginated — the platform-admin view of a tenant's assignments.
func (r *PackageRepo) FindByIDs(ctx context.Context, ids []primitive.ObjectID, page, limit int) ([]*models.Package, int64, error) {
	if len(ids) == 0 {
		return nil, 0, nil
	}
	return r.FindAll(ctx, bson.M{"_id": bson.M{"$in": ids}}, page, limit)
}

func (r *PackageRepo) Update(ctx context.Context, id primitive.ObjectID, update bson.M, userID *primitive.ObjectID) error {
	stripProtectedFields(update)
	update["updated_at"] = time.Now()
	if userID != nil {
		update["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": update})
	return err
}

func (r *PackageRepo) Delete(ctx context.Context, id primitive.ObjectID, userID *primitive.ObjectID) error {
	set := bson.M{"is_active": false, "updated_at": time.Now()}
	if userID != nil {
		set["user_id"] = *userID
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	return err
}
