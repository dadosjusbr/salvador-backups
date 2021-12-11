package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dadosjusbr/storage"
	"github.com/kelseyhightower/envconfig"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	mgoConnTimeout = 60 * time.Second
)

// O tipo decInt é necessário pois a biblioteca converte usando ParseInt passando
// zero na base. Ou seja, meses como 08 passam a ser inválidos pois são tratados
// como números octais.
type decInt int

func (i *decInt) Decode(value string) error {
	v, err := strconv.Atoi(value)
	*i = decInt(v)
	return err
}

type config struct {
	Month decInt `envconfig:"MONTH"`
	Year  decInt `envconfig:"YEAR"`
	AID   string `envconfig:"AID"`

	// Backup URL store
	MongoURI        string `envconfig:"MONGODB_URI"`
	MongoDBName     string `envconfig:"MONGODB_DBNAME"`
	MongoBackupColl string `envconfig:"MONGODB_BCOLL"`

	// Swift Conf
	SwiftUsername  string `envconfig:"SWIFT_USERNAME"`
	SwiftAPIKey    string `envconfig:"SWIFT_APIKEY"`
	SwiftAuthURL   string `envconfig:"SWIFT_AUTHURL"`
	SwiftDomain    string `envconfig:"SWIFT_DOMAIN"`
	SwiftContainer string `envconfig:"SWIFT_CONTAINER"`
}

func main() {
	// parsing environment variables.
	var conf config
	if err := envconfig.Process("", &conf); err != nil {
		log.Fatalf("Error loading config values from .env: %v", err)
	}
	conf.AID = strings.ToLower(conf.AID)

	// reading and parsing stdin.
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Error reading from stdin: %v", err)
	}
	paths := strings.Split(string(bytes.TrimRight(in, "\n")), "\n")

	// configuring mongodb and cloud backup clients.
	db, err := connect(conf.MongoURI)
	if err != nil {
		log.Fatalf("Error connecting to mongo: %v", err)
	}
	defer disconnect(db)
	dbColl := db.Database(conf.MongoDBName).Collection(conf.MongoBackupColl)

	cloud := storage.NewCloudClient(
		conf.SwiftUsername,
		conf.SwiftAPIKey,
		conf.SwiftAuthURL,
		conf.SwiftDomain,
		conf.SwiftContainer)

	backups, err := cloud.Backup(paths, conf.AID)
	if err != nil {
		log.Fatalf("Error backing up files %v:%v", paths, err)
	}

	_, err = dbColl.InsertOne(context.TODO(),
		bson.D{
			{Key: "aid", Value: conf.AID},
			{Key: "year", Value: conf.Year},
			{Key: "month", Value: conf.Month},
			{Key: "backups", Value: backups},
		})
	if err != nil {
		log.Fatalf("Error backups (%s, %d, %d, %+v) record in mongo:%v", conf.AID, conf.Year, conf.Month, backups, err)
	}

	// Printing the same input it gets. Acting as a proxy stage.
	fmt.Print(string(in))
}

func connect(url string) (*mongo.Client, error) {
	c, err := mongo.NewClient(options.Client().ApplyURI(url))
	if err != nil {
		return nil, fmt.Errorf("error creating mongo client(%s):%w", url, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), mgoConnTimeout)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		return nil, fmt.Errorf("error connecting to mongo(%s):%w", url, err)
	}
	return c, nil
}

func disconnect(c *mongo.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := c.Disconnect(ctx); err != nil {
		return fmt.Errorf("error disconnecting from mongo:%w", err)
	}
	return nil
}
