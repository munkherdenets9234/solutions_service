package repository

import (
	"context"
	"regexp"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type GuideApplicationRepo struct {
	col *mongo.Collection
}

func NewGuideApplicationRepo(db *mongo.Database) *GuideApplicationRepo {
	return &GuideApplicationRepo{col: db.Collection("guide_applications")}
}

// GuideListFilter narrows List. Empty fields are ignored.
type GuideListFilter struct {
	Status   models.GuideStatus
	Q        string
	Language string
	Region   string
}

func (r *GuideApplicationRepo) Create(ctx context.Context, tenantID primitive.ObjectID, a *models.GuideApplication) error {
	now := time.Now()
	a.ID = primitive.NewObjectID()
	a.TenantID = tenantID
	a.Status = models.GuideNew
	a.CreatedAt = now
	a.UpdatedAt = now
	// Non-nil so the document stores [] and $push always has an array to extend.
	a.Events = []models.GuideEvent{}
	_, err := r.col.InsertOne(ctx, a)
	return err
}

func (r *GuideApplicationRepo) FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.GuideApplication, error) {
	var a models.GuideApplication
	err := r.col.FindOne(ctx, bson.M{"_id": id, "tenant_id": tenantID}).Decode(&a)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *GuideApplicationRepo) List(ctx context.Context, tenantID primitive.ObjectID, f GuideListFilter, page, limit int) ([]*models.GuideApplication, int64, error) {
	filter := bson.M{"tenant_id": tenantID}
	if f.Status != "" {
		filter["status"] = f.Status
	}
	if f.Language != "" {
		filter["languages.language"] = f.Language
	}
	if f.Region != "" {
		filter["regions"] = f.Region
	}
	if f.Q != "" {
		re := primitive.Regex{Pattern: regexp.QuoteMeta(f.Q), Options: "i"}
		filter["$or"] = bson.A{
			bson.M{"personal.full_name": re},
			bson.M{"personal.phone": re},
			bson.M{"personal.email": re},
		}
	}

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

	var results []*models.GuideApplication
	if err := cur.All(ctx, &results); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *GuideApplicationRepo) CountByStatus(ctx context.Context, tenantID primitive.ObjectID) (map[models.GuideStatus]int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"tenant_id": tenantID}}},
		{{Key: "$group", Value: bson.M{"_id": "$status", "n": bson.M{"$sum": 1}}}},
	}
	cur, err := r.col.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var rows []struct {
		Status models.GuideStatus `bson:"_id"`
		N      int64              `bson:"n"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[models.GuideStatus]int64, len(rows))
	for _, row := range rows {
		out[row.Status] = row.N
	}
	return out, nil
}

// HasRecentByEmail reports whether the tenant already has an application for
// this season from this email since the given time. The caller normalises the
// email (trim, lower-case) to match how it was stored.
func (r *GuideApplicationRepo) HasRecentByEmail(ctx context.Context, tenantID primitive.ObjectID, season, email string, since time.Time) (bool, error) {
	filter := bson.M{
		"tenant_id":      tenantID,
		"season":         season,
		"personal.email": email,
		"created_at":     bson.M{"$gte": since},
	}
	n, err := r.col.CountDocuments(ctx, filter, options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetStatus changes the status and appends the timeline event in one write, so
// the two can never disagree. A foreign-tenant or unknown id matches nothing
// and reads as not found.
func (r *GuideApplicationRepo) SetStatus(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, status models.GuideStatus, ev models.GuideEvent) error {
	res, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "tenant_id": tenantID},
		bson.M{
			"$set":  bson.M{"status": status, "updated_at": time.Now()},
			"$push": bson.M{"events": ev},
		})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// AddNote appends a note event without touching the status.
func (r *GuideApplicationRepo) AddNote(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, ev models.GuideEvent) error {
	res, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "tenant_id": tenantID},
		bson.M{
			"$set":  bson.M{"updated_at": time.Now()},
			"$push": bson.M{"events": ev},
		})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// Delete removes one application. Used only to roll back a failed submit.
func (r *GuideApplicationRepo) Delete(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": id, "tenant_id": tenantID})
	return err
}
