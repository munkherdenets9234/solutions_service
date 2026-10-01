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
		{"tenant_details", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"tenant_details", mongo.IndexModel{Keys: bson.D{{Key: "showcase", Value: 1}, {Key: "sort_order", Value: 1}}}},
		{"destinations", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"blogs", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"cars", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"partners", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"customers", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"tenant_users", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"platform_users", mongo.IndexModel{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)}},
		// Reset lookups are by tenant and email, newest first.
		{"tenant_password_resets", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "email", Value: 1}, {Key: "created_at", Value: -1}}}},
		// A TTL index, so spent codes delete themselves an hour past expiry.
		// A reset code is a credential, and a collection of old ones is a
		// collection of hashed credentials sitting in every backup for no
		// reason. The hour of slack means a stale code is answered "expired"
		// rather than "no such code", which is the more useful thing to tell
		// someone typing one.
		{"tenant_password_resets", mongo.IndexModel{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(3600)}},
		{"bookings", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		{"rentals", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		{"airport_transfers", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		{"contact_messages", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		{"quotes", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		{"newsletter_subscribers", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"reviews", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		{"packages", mongo.IndexModel{Keys: bson.D{{Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{"tenant_reviews", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		{"tenant_packages", mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "package_id", Value: 1}}, Options: options.Index().SetUnique(true)}},
	}

	for _, s := range specs {
		if _, err := db.Collection(s.collection).Indexes().CreateOne(ctx, s.model); err != nil {
			return err
		}
	}
	return nil
}
