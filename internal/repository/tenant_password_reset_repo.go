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

type TenantPasswordResetRepo struct {
	col *mongo.Collection
}

func NewTenantPasswordResetRepo(db *mongo.Database) *TenantPasswordResetRepo {
	return &TenantPasswordResetRepo{col: db.Collection("tenant_password_resets")}
}

// activeFilter finds the unused, unexpired codes for one tenant's address.
//
// The tenant is part of the filter on purpose. Users are unique per tenant, not
// globally, so two tenants can each have a user with the same email, and a code
// issued for one must never be findable for the other.
func activeFilter(tenantID primitive.ObjectID, email string, now time.Time) bson.M {
	return bson.M{
		"tenant_id":  tenantID,
		"email":      email,
		"used_at":    bson.M{"$exists": false},
		"expires_at": bson.M{"$gt": now},
	}
}

// invalidateFilter finds a user's outstanding codes.
func invalidateFilter(tenantID, userID primitive.ObjectID) bson.M {
	return bson.M{
		"tenant_id": tenantID,
		"user_id":   userID,
		"used_at":   bson.M{"$exists": false},
	}
}

// Create stores a new code after invalidating any outstanding one for the same
// user. Only the newest code is ever valid: otherwise an account with three
// requested codes has three live credentials, and the oldest is the one most
// likely to have been forwarded.
func (r *TenantPasswordResetRepo) Create(ctx context.Context, p *models.TenantPasswordReset) error {
	if err := r.InvalidateForUser(ctx, p.TenantID, p.UserID); err != nil {
		return err
	}
	p.CreatedAt = time.Now()
	res, err := r.col.InsertOne(ctx, p)
	if err != nil {
		return err
	}
	p.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

// FindActive returns the newest unused, unexpired code for the address. Sorted
// newest first rather than assuming one exists: Create invalidates earlier
// codes, but a crash between its two writes would leave two, and picking an
// arbitrary one is how a burned code starts working again.
func (r *TenantPasswordResetRepo) FindActive(ctx context.Context, tenantID primitive.ObjectID, email string) (*models.TenantPasswordReset, error) {
	var p models.TenantPasswordReset
	err := r.col.FindOne(ctx, activeFilter(tenantID, email, time.Now()),
		options.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}}),
	).Decode(&p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// RecordAttempt counts a wrong guess. It always persists, including when the
// guess is wrong: a counter that only advances on some paths is a counter an
// attacker can avoid.
func (r *TenantPasswordResetRepo) RecordAttempt(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.col.UpdateByID(ctx, id, bson.M{"$inc": bson.M{"attempts": 1}})
	return err
}

// MarkUsed consumes a code, conditional on it still being unused, so two
// confirms racing with the same code cannot both succeed.
func (r *TenantPasswordResetRepo) MarkUsed(ctx context.Context, id primitive.ObjectID) (bool, error) {
	res, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "used_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"used_at": time.Now()}},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}

// InvalidateForUser burns every outstanding code for a user. Called when a new
// one is issued, and after a successful reset: changing a password should not
// leave a working way to change it again.
func (r *TenantPasswordResetRepo) InvalidateForUser(ctx context.Context, tenantID, userID primitive.ObjectID) error {
	_, err := r.col.UpdateMany(ctx, invalidateFilter(tenantID, userID),
		bson.M{"$set": bson.M{"used_at": time.Now()}})
	return err
}
