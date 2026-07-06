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

type PlatformUserRepo struct {
	col *mongo.Collection
}

func NewPlatformUserRepo(db *mongo.Database) *PlatformUserRepo {
	return &PlatformUserRepo{col: db.Collection("platform_users")}
}

func (r *PlatformUserRepo) Create(ctx context.Context, u *models.PlatformUser) error {
	u.ID = primitive.NewObjectID()
	u.Status = models.PlatformUserActive
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, u)
	return err
}

func (r *PlatformUserRepo) FindAll(ctx context.Context, page, limit int) ([]*models.PlatformUser, int64, error) {
	total, err := r.col.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, err
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().SetSkip(skip).SetLimit(int64(limit)).SetSort(bson.D{{Key: "created_at", Value: -1}})

	cur, err := r.col.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)

	var results []*models.PlatformUser
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *PlatformUserRepo) FindByEmail(ctx context.Context, email string) (*models.PlatformUser, error) {
	var u models.PlatformUser
	err := r.col.FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *PlatformUserRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*models.PlatformUser, error) {
	var u models.PlatformUser
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *PlatformUserRepo) UpdatePassword(ctx context.Context, id primitive.ObjectID, passwordHash string) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"password_hash": passwordHash,
		"updated_at":    time.Now(),
	}})
	return err
}

func (r *PlatformUserRepo) CountActive(ctx context.Context) (int64, error) {
	return r.col.CountDocuments(ctx, bson.M{"status": models.PlatformUserActive})
}

func (r *PlatformUserRepo) UpdateStatus(ctx context.Context, id primitive.ObjectID, status models.PlatformUserStatus) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"status":     status,
		"updated_at": time.Now(),
	}})
	return err
}
