package repository

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// EnsureIndexes creates the indexes multi-tenancy relies on: tenants are
// looked up by slug/api_key_hash globally, while slugs and customer emails
// only need to be unique within a single tenant.
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	specs := []struct {
		collection string
		model      mongo.IndexModel
	}{
		{"tenants", mongo.IndexModel{Keys: bson.D{{Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"tenants", mongo.IndexModel{Keys: bson.D{{Key: "api_key_hash", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"tenants", mongo.IndexModel{Keys: bson.D{{Key: "domain", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true)}},
		{"destinations", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"blogs", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"cars", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"partners", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"customers", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"tenant_users", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"platform_users", mongo.IndexModel{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"subscriptions", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"bookings", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		{"rentals", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		{"airport_transfers", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		{"contact_messages", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		{"reviews", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
	}

	for _, s := range specs {
		if _, err := db.Collection(s.collection).Indexes().CreateOne(ctx, s.model); err != nil {
			return err
		}
	}
	return nil
}
