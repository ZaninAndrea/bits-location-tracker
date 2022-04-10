package main

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/umahmood/haversine"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Place struct {
	Center haversine.Coord
	Radius float64
	Name   string
}

func main() {
	godotenv.Load()

	ctx, release := context.WithTimeout(context.Background(), 10*time.Second)
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(os.Getenv("MONGO_URI")))
	release()

	if err != nil {
		panic(err)
	}

	places := LoadPlaces(client)

	// Reload places array periodically
	go func() {
		for {
			time.Sleep(6 * time.Hour)
			places = LoadPlaces(client)
		}
	}()

	r := gin.Default()
	r.POST("/log", func(c *gin.Context) {
		lat, err := strconv.ParseFloat(c.Query("lat"), 64)
		if err != nil {
			c.String(400, "Badly formatted lat")
			return
		}

		lon, err := strconv.ParseFloat(c.Query("lon"), 64)
		if err != nil {
			c.String(400, "Badly formatted lon")
			return
		}

		acc, err := strconv.ParseFloat(c.Query("acc"), 64)
		if err != nil {
			c.String(400, "Badly formatted lon")
			return
		}

		timestamp := c.Query("time")

		// Ignore positions with more than 70m of error
		if acc > 70 {
			return
		}

		RecordNewPosition(client, MatchLocation(lat, lon, acc, places), timestamp)

		ctx, release = context.WithTimeout(context.Background(), 10*time.Second)
		client.Database("v1").Collection("config").UpdateOne(ctx, bson.M{}, bson.M{
			"$set": bson.M{
				"collections.location.lastFetched": time.Now(),
			},
		},
		)
		release()

		c.String(200, "")
	})
	r.Run(":" + os.Getenv("PORT"))
}

func MatchLocation(lat float64, lon float64, acc float64, places []Place) string {
	currentPosition := haversine.Coord{Lat: lat, Lon: lon}

	for _, place := range places {
		if Distance(place.Center, currentPosition) < place.Radius+acc {
			return place.Name
		}
	}

	return "World"
}

func Distance(a haversine.Coord, b haversine.Coord) float64 {
	_, km := haversine.Distance(a, b)
	return 1000 * km
}

func RecordNewPosition(client *mongo.Client, location string, timestamp string) {
	recordDate, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		panic(err)
	}

	lastPosition := GetLastPosition(client)

	if lastPosition != nil && lastPosition.EndDate.After(recordDate) {
		// Ignore out of order records
		return
	}

	if lastPosition == nil || lastPosition.Location != location || recordDate.Sub(lastPosition.EndDate) > 3*time.Hour {
		// Create a new position record

		ctx, release := context.WithTimeout(context.Background(), 10*time.Second)
		_, err = client.Database("v1").Collection("location").
			InsertOne(ctx, bson.M{
				"location":   location,
				"startDate":  recordDate,
				"endDate":    recordDate,
				"duration":   0,
				"_fetchDate": time.Now(),
			})
		release()

		if err != nil {
			panic(err)
		}
	} else {
		// Update old position record
		findOptions := options.Update()
		ctx, release := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := client.Database("v1").Collection("location").
			UpdateOne(ctx, bson.M{
				"_id": lastPosition.Id,
			}, bson.M{
				"$set": bson.M{
					"endDate":  recordDate,
					"duration": recordDate.Sub(lastPosition.StartDate).Milliseconds(),
				},
			}, findOptions)
		release()

		if err != nil {
			panic(err)
		}
	}

}

type PositionRecord struct {
	Id        primitive.ObjectID `bson:"_id"`
	Location  string
	StartDate time.Time
	EndDate   time.Time
}

func GetLastPosition(client *mongo.Client) *PositionRecord {
	findOptions := options.FindOne()
	findOptions.SetSort(bson.D{{"startDate", -1}})

	ctx, release := context.WithTimeout(context.Background(), 10*time.Second)
	res := client.Database("v1").Collection("location").
		FindOne(ctx, bson.M{}, findOptions)
	release()

	var record PositionRecord
	err := res.Decode(&record)
	if err != nil {
		if err.Error() == "mongo: no documents in result" {
			return nil
		}

		panic(err)
	}

	return &record
}

func LoadPlaces(client *mongo.Client) []Place {
	ctx, release := context.WithTimeout(context.Background(), 10*time.Second)
	res := client.Database("v1").Collection("config").
		FindOne(ctx, bson.M{})
	release()

	var config struct {
		Collections struct {
			Location struct {
				Internal struct {
					Places []Place
				}
			}
		}
	}
	err := res.Decode(&config)
	if err != nil {
		panic(err)
	}

	return config.Collections.Location.Internal.Places
}
