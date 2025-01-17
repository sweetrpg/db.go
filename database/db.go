package database

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/sweetrpg/common/logging"
	"github.com/sweetrpg/db/constants"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var Db *mongo.Database
var client *mongo.Client

func buildDbURL() (*url.URL, string) {
	dbUri, found := os.LookupEnv(constants.DB_URI)
	if found {
		dbUrl, err := url.Parse(dbUri)
		if err != nil {
			panic(err)
		}

		return dbUrl, dbUrl.Path[1:]
	}

	dbScheme := os.Getenv(constants.DB_SCHEME)
	dbUser := os.Getenv(constants.DB_USER)
	dbPW := os.Getenv(constants.DB_PW)
	dbHost := os.Getenv(constants.DB_HOST)
	dbPort, portFound := os.LookupEnv(constants.DB_PORT)
	dbOpts := os.Getenv(constants.DB_OPTS)
	dbName := os.Getenv(constants.DB_NAME)

	var host string
	if portFound {
		host = fmt.Sprintf("%s:%s", dbHost, dbPort)
	} else {
		host = dbHost
	}

	dbUrl := &url.URL{
		Scheme:     dbScheme,
		Host:       host,
		User:       url.UserPassword(dbUser, dbPW),
		Path:       dbName,
		RawQuery:   dbOpts,
		ForceQuery: true,
	}

	return dbUrl, dbName
}

func SetupDatabase() {
	dbUrl, dbName := buildDbURL()
	logging.Logger.Info("Connecting to database", "url", dbUrl.Redacted())
	client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(dbUrl.String()))
	if err != nil {
		panic(err)
	}

	logging.Logger.Info("Setting up database", "dbName", dbName)
	Db = client.Database(dbName)
}

func TeardownDatabase() {
	if client != nil {
		if err := client.Disconnect(context.TODO()); err != nil {
			logging.Logger.Error("Error while disconnecting from database", "error", err.Error())
		}
	}
}

func Get[T any](collection string, id primitive.ObjectID) (*T, error) {
	logging.Logger.Debug(fmt.Sprintf("Using '%s' collection on DB", collection))
	coll := Db.Collection(collection)
	logging.Logger.Debug(fmt.Sprintf("collection=%v", coll)) // TODO: remove

	// objectId, err := primitive.ObjectIDFromHex(id)
	// if err != nil {
	// 	logging.Logger.Error(fmt.Sprintf("Unable to create ObjectID from %s: %s", id, err.Error()))
	// 	return nil, err
	// }
	filter := bson.D{{Key: "_id", Value: id}}
	var model T
	err := coll.FindOne(context.TODO(), filter).Decode(&model)
	// bsonBytes, err := bson.Marshal(result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}

		logging.Logger.Error(fmt.Sprintf("Failed to marshal BSON: %v", err))
		return nil, err
	}
	// logging.Logger.Debug(fmt.Sprintf("bsonBytes=%v", bsonBytes))

	// if err := bson.Unmarshal(bsonBytes, &model); err != nil {
	// 	logging.Logger.Error(fmt.Sprintf("Failed to unmarshal BSON to struct: %v", err))
	// 	return nil, err
	// }
	logging.Logger.Debug(fmt.Sprintf("model=%v", model))

	return &model, nil
}

// Query the database for multiple documents.
//
// @Param collection The name of the collection to query.
//
// @Param filter A BSON document specifying a filter to apply to the query.
//
// @Param sort A BSON document specifying how to sort the returned results.
//
//	{'field': order}
//	where 'field' is the name of the field in the database, and <order> is an integer value specifying
//	whether the field should be sorted ascending (1) or descending (-1)
//
// @Param projection A BSON document specifying a specific set of fields to return or ignore
//
//	{'field': value}
//	where 'field' is the name of the field in the database, and <value> is an integer value specifying
//	whether the field should be returned (1, excluding others) or ignored (0)
//
// @Param start The starting document for the query results.
//
// @Param limit The maximum number of documents to return in the query.
//
// @Return An array of the documents matching the query parameters, or an error.
func Query[T any](collection string, filter bson.D, sort bson.D, projection bson.D, start int64, limit int) ([]*T, error) {
	logging.Logger.Debug(fmt.Sprintf("Using '%s' collection on DB", collection),
		"filter", filter,
		"sort", sort,
		"projection", projection,
		"start", start,
		"limit", limit)
	coll := Db.Collection(collection)

	logging.Logger.Info(fmt.Sprintf("Querying for '%s'...", collection))
	// sortStage := bson.D{{"$sort", sort}}
	// logging.Logger.Debug(fmt.Sprintf("sort=%v", sortStage))
	// skipStage := bson.D{{"$skip", math.Max(0, float64(start))}}
	// logging.Logger.Debug(fmt.Sprintf("skip=%v", skipStage))
	// limitStage := bson.D{{"$limit", int(math.Max(0, math.Min(float64(limit), float64(constants.QueryMaxSize))))}}
	// logging.Logger.Debug(fmt.Sprintf("limit=%v", limitStage))
	// pipeline := mongo.Pipeline{sortStage, skipStage, limitStage}

	// If no sort key is specified, sort by ID
	// if len(sort) == 0 {
	// 	logging.Logger.Info("Set default sort on _id, since no sort was specified.")
	// 	sort = bson.D{{"_id", 1}}
	// }

	opts := options.Find().
		SetSort(sort).
		SetSkip(start).
		SetLimit(int64(limit))
	logging.Logger.Debug("find options", "opts", opts)

	if len(projection) > 0 {
		logging.Logger.Info("Setting projection", "projection", projection)
		opts.SetProjection(projection)
	}

	cursor, err := coll.Find(context.TODO(), filter, opts)
	logging.Logger.Debug("find results", "cursor", cursor, "err", err)
	if err != nil {
		logging.Logger.Error(fmt.Sprintf("Error while trying to find '%s' documents", collection), "error", err)
		return nil, err
	}

	var results []*T
	err = cursor.All(context.TODO(), &results)
	logging.Logger.Debug("cursor.All", "results", results, "err", err)
	if err != nil {
		logging.Logger.Error(fmt.Sprintf("Error while trying to fetch '%s' documents", collection), "error", err)
		return nil, err
	}

	logging.Logger.Debug("query results", "results", results)
	var models []*T
	for _, r := range results {
		logging.Logger.Debug(fmt.Sprintf("r=%v", r))
		var model *T
		bsonBytes, err := bson.Marshal(r)
		if err != nil {
			logging.Logger.Error(fmt.Sprintf("Failed to marshal BSON: %v", err))
		}
		logging.Logger.Debug(fmt.Sprintf("bsonBytes=%v", bsonBytes))

		if err := bson.Unmarshal(bsonBytes, &model); err != nil {
			logging.Logger.Error(fmt.Sprintf("Failed to unmarshal BSON to struct: %v", err))
		}

		logging.Logger.Debug("appending model", "model", model)
		models = append(models, model)
	}
	// err = bson.Unmarshal(result, &licenses)

	logging.Logger.Debug("returning", "models", models)
	return models, nil
}

func Insert[T any](collection string, doc T) (primitive.ObjectID, error) {
	logging.Logger.Debug(fmt.Sprintf("Using '%s' collection on DB", collection),
		"doc", doc)
	coll := Db.Collection(collection)

	logging.Logger.Info(fmt.Sprintf("Inserting new document into collection '%s'...", collection))

	result, err := coll.InsertOne(context.TODO(), doc)
	if err != nil {
		logging.Logger.Error("Error while trying to insert documents into collection", "collection", collection, "error", err)
		return primitive.NilObjectID, err
	}

	id, ok := result.InsertedID.(primitive.ObjectID)
	logging.Logger.Info("Document inserted", "id", id, "ok", ok)
	return id, nil
}

func Update[T any](collection string, id primitive.ObjectID, doc T) (int, int, error) {
	logging.Logger.Debug(fmt.Sprintf("Using '%s' collection on DB", collection),
		"id", id,
		"doc", doc)
	coll := Db.Collection(collection)

	filter := bson.D{{Key: "_id", Value: id}}

	data, err := bson.Marshal(doc) //   D{} // TODO:
	if err != nil {
		logging.Logger.Error("Error while trying prepare document for update in collection", "collection", collection, "id", id, "error", err)
		return 0, 0, err
	}
	logging.Logger.Debug("marshal document", "data", data)
	var update bson.D
	err = bson.Unmarshal(data, &update)
	logging.Logger.Debug("unmarshaled", "update", update, "err", err)
	if err != nil {
		logging.Logger.Error("Error while trying prepare document for update in collection", "collection", collection, "id", id, "data", data, "error", err)
		return 0, 0, err
	}
	logging.Logger.Debug("unmarshal document", "updates", update)

	result, err := coll.UpdateOne(context.TODO(), filter, bson.D{{Key: "$set", Value: update}})
	if err != nil {
		logging.Logger.Error("Error while trying to update document in collection", "collection", collection, "id", id, "error", err)
		return 0, 0, err
	}

	logging.Logger.Info("Document updated", "id", id, "matched", result.MatchedCount, "modified", result.ModifiedCount)
	return int(result.MatchedCount), int(result.ModifiedCount), nil
}

func Delete[T any](collection string, id primitive.ObjectID) (bool, error) {
	logging.Logger.Debug(fmt.Sprintf("Using '%s' collection on DB", collection),
		"id", id)
	coll := Db.Collection(collection)

	filter := bson.D{{Key: "_id", Value: id}}

	result, err := coll.DeleteOne(context.TODO(), filter)
	if err != nil {
		logging.Logger.Error("Error while trying to delete documents from collection", "collection", collection, "id", id, "error", err)
		return false, err
	}

	logging.Logger.Info("Deleted document", "count", result.DeletedCount)
	return (result.DeletedCount > 0), nil
}
