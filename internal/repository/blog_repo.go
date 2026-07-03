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

type BlogRepo struct {
	col *mongo.Collection
}

func NewBlogRepo(db *mongo.Database) *BlogRepo {
	return &BlogRepo{col: db.Collection("blogs")}
}

func (r *BlogRepo) Create(ctx context.Context, tenantID primitive.ObjectID, b *models.Blog) error {
	b.ID = primitive.NewObjectID()
	b.TenantID = tenantID
	b.Status = models.BlogDraft
	b.Views = 0
	b.CreatedAt = time.Now()
	b.UpdatedAt = time.Now()
	_, err := r.col.InsertOne(ctx, b)
	return err
}

func (r *BlogRepo) FindPublished(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.Blog, int64, error) {
	filter := bson.M{"tenant_id": tenantID, "status": models.BlogPublished}
	total, err := r.col.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().
		SetSkip(skip).
		SetLimit(int64(limit)).
		SetSort(bson.D{{Key: "published_at", Value: -1}}).
		SetProjection(bson.M{"content": 0}) // exclude full content in list

	cur, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)

	var results []*models.Blog
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *BlogRepo) FindBySlug(ctx context.Context, tenantID primitive.ObjectID, slug string) (*models.Blog, error) {
	var b models.Blog
	err := r.col.FindOne(ctx, bson.M{"tenant_id": tenantID, "slug": slug, "status": models.BlogPublished}).Decode(&b)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *BlogRepo) FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.Blog, error) {
	var b models.Blog
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&b)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *BlogRepo) Update(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, update bson.M) error {
	update["updated_at"] = time.Now()
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$set": update})
	return err
}

func (r *BlogRepo) IncrementViews(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}, bson.M{"$inc": bson.M{"views": 1}})
	return err
}
