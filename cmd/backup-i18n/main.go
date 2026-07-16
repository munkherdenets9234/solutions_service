// Command backup-i18n dumps the collections that cmd/migrate-i18n touches
// (blogs, partners, destinations, reviews) to local JSON files before running
// the migration, so it can be restored if anything goes wrong.
//
// Each collection is written as a JSON array of documents in MongoDB
// Extended JSON (canonical) form, which round-trips ObjectIDs, dates, etc.
// exactly - safe to feed back into mongoimport or a small restore script.
//
//	go run ./cmd/backup-i18n                  # writes to ./backups/<timestamp>/
//	go run ./cmd/backup-i18n -out mybackup    # writes to ./mybackup/
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var collections = []string{"blogs", "partners", "destinations", "reviews"}

func main() {
	out := flag.String("out", "", "output directory (default: ./backups/<timestamp>)")
	flag.Parse()

	_ = godotenv.Load()
	cfg := config.Load()

	outDir := *out
	if outDir == "" {
		outDir = filepath.Join("backups", time.Now().Format("2006-01-02T150405"))
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("mkdir %s failed: %v", outDir, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Fatalf("mongo connect failed: %v", err)
	}
	defer func() { _ = client.Disconnect(ctx) }()
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("mongo ping failed: %v", err)
	}
	db := client.Database(cfg.MongoDB)

	fmt.Printf("backup-i18n: db=%s -> %s\n", cfg.MongoDB, outDir)

	for _, name := range collections {
		count := backupCollection(ctx, db, name, outDir)
		fmt.Printf("%s: backed up %d document(s)\n", name, count)
	}

	fmt.Println("backup-i18n: done")
}

func backupCollection(ctx context.Context, db *mongo.Database, name, outDir string) int {
	col := db.Collection(name)
	cur, err := col.Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("%s: find failed: %v", name, err)
	}
	defer cur.Close(ctx)

	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		log.Fatalf("%s: decode failed: %v", name, err)
	}

	// Extended JSON preserves ObjectID/date/etc. types exactly, unlike
	// encoding/json on a bson.M (which would mangle them into plain strings).
	// MarshalExtJSON only accepts a single document, not a top-level array,
	// so each document is marshaled individually and joined by hand.
	var buf bytes.Buffer
	buf.WriteString("[\n")
	for i, doc := range docs {
		data, err := bson.MarshalExtJSONIndent(doc, false, false, "  ", "  ")
		if err != nil {
			log.Fatalf("%s: marshal doc %d failed: %v", name, i, err)
		}
		buf.WriteString("  ")
		buf.Write(data)
		if i < len(docs)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("]\n")

	path := filepath.Join(outDir, name+".json")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		log.Fatalf("%s: write failed: %v", name, err)
	}

	return len(docs)
}
