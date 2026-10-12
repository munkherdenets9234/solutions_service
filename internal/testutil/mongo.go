// Package testutil connects integration tests to a real MongoDB server
// (named by TEST_MONGO_URI) and wires the full application on top of it, so
// tests exercise the real HTTP router, auth middleware, and Mongo queries
// end-to-end rather than mocked pieces.
package testutil

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestDBName is the only database the harness ever touches. It is emptied by
// dropping its collections; the database itself is never dropped, and no other
// database on the server is read or written.
const TestDBName = "tpl_test_run"

// StartMongo connects to the server named by TEST_MONGO_URI, empties the
// TestDBName database (collections only) and returns a handle scoped to it.
// The database is emptied again on cleanup. The test is skipped when
// TEST_MONGO_URI is unset.
func StartMongo(t testing.TB) *mongo.Database {
	t.Helper()

	uri := os.Getenv("TEST_MONGO_URI")
	if uri == "" {
		t.Skip("TEST_MONGO_URI not set; integration tests need a MongoDB server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("mongo ping: %v", err)
	}

	db := client.Database(TestDBName)
	wipe(t, db)
	t.Cleanup(func() {
		wipe(t, db)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Disconnect(ctx)
	})

	return db
}

// wipe drops every collection in db. It refuses any database other than
// TestDBName.
func wipe(t testing.TB, db *mongo.Database) {
	t.Helper()
	if db.Name() != TestDBName {
		t.Fatalf("refusing to wipe database %q", db.Name())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		t.Fatalf("list collections: %v", err)
	}
	for _, n := range names {
		if err := db.Collection(n).Drop(ctx); err != nil {
			t.Fatalf("drop collection %q: %v", n, err)
		}
	}
}
