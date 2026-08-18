package repository

import (
	"context"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type TenantPackageRepo struct {
	col *mongo.Collection
}

func NewTenantPackageRepo(db *mongo.Database) *TenantPackageRepo {
	return &TenantPackageRepo{col: db.Collection("tenant_packages")}
}

func (r *TenantPackageRepo) Assign(ctx context.Context, tenantID, packageID primitive.ObjectID, userID *primitive.ObjectID) error {
	tp := &models.TenantPackage{
		ID:        primitive.NewObjectID(),
		TenantID:  tenantID,
		PackageID: packageID,
		CreatedAt: time.Now(),
		UserID:    userID,
	}
	_, err := r.col.InsertOne(ctx, tp)
	return err
}

func (r *TenantPackageRepo) Unassign(ctx context.Context, tenantID, packageID primitive.ObjectID) (int64, error) {
	res, err := r.col.DeleteOne(ctx, bson.M{"tenant_id": tenantID, "package_id": packageID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// FindPackageIDsByTenant returns every package_id assigned to a tenant.
func (r *TenantPackageRepo) FindPackageIDsByTenant(ctx context.Context, tenantID primitive.ObjectID) ([]primitive.ObjectID, error) {
	cur, err := r.col.Find(ctx, bson.M{"tenant_id": tenantID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var links []models.TenantPackage
	if err := cur.All(ctx, &links); err != nil {
		return nil, err
	}
	ids := make([]primitive.ObjectID, len(links))
	for i, l := range links {
		ids[i] = l.PackageID
	}
	return ids, nil
}
