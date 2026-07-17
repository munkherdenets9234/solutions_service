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

type TenantUserRepo struct {
	col *mongo.Collection
}

func NewTenantUserRepo(db *mongo.Database) *TenantUserRepo {
	return &TenantUserRepo{col: db.Collection("tenant_users")}
}

func (r *TenantUserRepo) Create(ctx context.Context, u *models.TenantUser) error {
	u.ID = primitive.NewObjectID()
	u.Status = models.TenantUserActive
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, u)
	return err
}

func (r *TenantUserRepo) FindAll(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.TenantUser, int64, error) {
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

	var results []*models.TenantUser
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *TenantUserRepo) FindByTenantAndEmail(ctx context.Context, tenantID primitive.ObjectID, email string) (*models.TenantUser, error) {
	var u models.TenantUser
	err := r.col.FindOne(ctx, bson.M{"tenant_id": tenantID, "email": email}).Decode(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *TenantUserRepo) FindByID(ctx context.Context, tenantID, id primitive.ObjectID) (*models.TenantUser, error) {
	var u models.TenantUser
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByIDs bulk-resolves tenant users for list views that need to display
// several audit-trail names at once without an N+1 query per row.
func (r *TenantUserRepo) FindByIDs(ctx context.Context, tenantID primitive.ObjectID, ids []primitive.ObjectID) ([]*models.TenantUser, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	cur, err := r.col.Find(ctx, bson.M{"tenant_id": tenantID, "_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var results []*models.TenantUser
	if err := cur.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *TenantUserRepo) UpdatePassword(ctx context.Context, tenantID, id primitive.ObjectID, passwordHash string) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": bson.M{
		"password_hash": passwordHash,
		"updated_at":    time.Now(),
	}})
	return err
}

func (r *TenantUserRepo) UpdateStatus(ctx context.Context, tenantID, id primitive.ObjectID, status models.TenantUserStatus) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": bson.M{
		"status":     status,
		"updated_at": time.Now(),
	}})
	return err
}
