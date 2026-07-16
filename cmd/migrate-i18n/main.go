// Command migrate-i18n wraps existing flat string / string-list field values
// into {"en": <value>} locale maps across the blogs, partners, destinations,
// and reviews collections, matching the shape internal/models now expects
// after the i18n migration.
//
// Idempotent: any field that is already an object (i.e. already migrated) is
// left untouched, so this is safe to re-run.
//
// By default this only prints a dry-run report. Pass -apply to actually
// write changes:
//
//	go run ./cmd/migrate-i18n            # dry run
//	go run ./cmd/migrate-i18n -apply      # writes changes
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/eandstravel/digitalservice/internal/config"
	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default is a dry run that only reports what would change)")
	flag.Parse()

	_ = godotenv.Load()
	cfg := config.Load()

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

	fmt.Printf("migrate-i18n: db=%s apply=%v\n", cfg.MongoDB, *apply)

	migrateCollection(ctx, db, "blogs", []string{"title", "excerpt", "content", "quote"}, nil, *apply)
	migrateCollection(ctx, db, "partners", []string{"title", "description"}, nil, *apply)
	migrateCollection(ctx, db, "destinations",
		[]string{"overview", "accommodation", "meal_plan", "difficulty"},
		[]string{"highlights", "activities", "inclusions", "exclusions"},
		*apply,
	)
	migrateCollection(ctx, db, "reviews", []string{"review"}, nil, *apply)
	migrateDestinationItinerary(ctx, db, *apply)

	fmt.Println("migrate-i18n: done")
}

// migrateCollection wraps the given top-level string fields and string-list
// fields into {"en": value} for every document in the collection.
func migrateCollection(ctx context.Context, db *mongo.Database, collection string, stringFields, listFields []string, apply bool) {
	col := db.Collection(collection)
	cur, err := col.Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("%s: find failed: %v", collection, err)
	}
	defer cur.Close(ctx)

	scanned, changed := 0, 0
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			log.Fatalf("%s: decode failed: %v", collection, err)
		}
		scanned++

		changes := bson.M{}
		for _, f := range stringFields {
			if v, ok := wrapString(doc[f]); ok {
				changes[f] = v
			}
		}
		for _, f := range listFields {
			if v, ok := wrapList(doc[f]); ok {
				changes[f] = v
			}
		}

		if len(changes) == 0 {
			continue
		}
		changed++
		id := doc["_id"]
		fmt.Printf("%s: %v -> wrapping fields %v\n", collection, id, changesKeys(changes))
		if apply {
			if _, err := col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": changes}); err != nil {
				log.Fatalf("%s: update failed for %v: %v", collection, id, err)
			}
		}
	}
	if err := cur.Err(); err != nil {
		log.Fatalf("%s: cursor error: %v", collection, err)
	}
	fmt.Printf("%s: scanned=%d changed=%d\n", collection, scanned, changed)
}

// migrateDestinationItinerary rewrites the whole `itinerary` array on every
// destination, wrapping each day's Title/Description/Overnight (string) and
// Activities/Meals (string list) sub-fields into locale maps.
func migrateDestinationItinerary(ctx context.Context, db *mongo.Database, apply bool) {
	col := db.Collection("destinations")
	cur, err := col.Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("destinations.itinerary: find failed: %v", err)
	}
	defer cur.Close(ctx)

	scanned, changed := 0, 0
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			log.Fatalf("destinations.itinerary: decode failed: %v", err)
		}
		scanned++

		rawDays, ok := doc["itinerary"].(bson.A)
		if !ok || len(rawDays) == 0 {
			continue
		}

		didChange := false
		newDays := make(bson.A, len(rawDays))
		for i, rd := range rawDays {
			day, ok := rd.(bson.M)
			if !ok {
				newDays[i] = rd
				continue
			}
			for _, f := range []string{"title", "description", "overnight"} {
				if v, ok := wrapString(day[f]); ok {
					day[f] = v
					didChange = true
				}
			}
			for _, f := range []string{"activities", "meals"} {
				if v, ok := wrapList(day[f]); ok {
					day[f] = v
					didChange = true
				}
			}
			newDays[i] = day
		}

		if !didChange {
			continue
		}
		changed++
		id := doc["_id"]
		fmt.Printf("destinations.itinerary: %v -> wrapping %d day(s)\n", id, len(newDays))
		if apply {
			if _, err := col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"itinerary": newDays}}); err != nil {
				log.Fatalf("destinations.itinerary: update failed for %v: %v", id, err)
			}
		}
	}
	if err := cur.Err(); err != nil {
		log.Fatalf("destinations.itinerary: cursor error: %v", err)
	}
	fmt.Printf("destinations.itinerary: scanned=%d changed=%d\n", scanned, changed)
}

// wrapString returns ({"en": v}, true) if v is a plain (non-empty) string
// that still needs wrapping. Anything else (already a map, missing, empty,
// wrong type) is left untouched — that's what makes this idempotent.
func wrapString(v interface{}) (bson.M, bool) {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil, false
	}
	return bson.M{i18n.DefaultLocale: s}, true
}

// wrapList is wrapString for array fields (Highlights, Activities, ...).
func wrapList(v interface{}) (bson.M, bool) {
	a, ok := v.(bson.A)
	if !ok || len(a) == 0 {
		return nil, false
	}
	return bson.M{i18n.DefaultLocale: a}, true
}

func changesKeys(m bson.M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
