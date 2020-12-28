package scalers

import (
	"context"
	"errors"
	"fmt"
	kedautil "github.com/kedacore/keda/pkg/util"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/x/bsonx"
	"k8s.io/api/autoscaling/v2beta2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/metrics/pkg/apis/external_metrics"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"
	"time"
)

// mongoScaler is support for mongo in keda.
type mongoScaler struct {
	metadata *mongoMetadata
	client   *mongo.Client
}

// mongoMetadata specify mongo scaler params.
type mongoMetadata struct {
	// The string is used by connected with mongo.
	// +optional
	connectionString string
	// Specify the host to connect to the mongo server,if the connectionString be provided, don't need specify this param.
	// +optional
	host string
	// Specify the port to connect to the mongo server,if the connectionString be provided, don't need specify this param.
	// +optional
	port string
	// Specify the username to connect to the mongo server,if the connectionString be provided, don't need specify this param.
	// +optional
	username string
	// Specify the password to connect to the mongo server,if the connectionString be provided, don't need specify this param.
	// +optional
	password string

	// The name of the database to be queried.
	// +required
	dbName string
	// The name of the collection to be queried.
	// +required
	collection string
	// A mongo filter doc,used by specify DB.
	// +required
	query string
	// A threshold that is used as targetAverageValue in HPA
	// +required
	queryValue int
}

// Default variables and settings
const (
	mongoDefaultTimeOut = 10 * time.Second
	defaultCollection   = "default"
	defaultDB           = "local"
	defaultQueryValue   = 1
)

type MongoFields struct {
	ID primitive.ObjectID `bson:"_id, omitempty"`
}

var mongoLog = logf.Log.WithName("mongo_scaler")

// NewMongoScaler creates a new Mongo scaler
func NewMongoScaler(config *ScalerConfig) (Scaler, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mongoDefaultTimeOut)
	defer cancel()

	meta, connStr, err := parseMongoMetadata(config)
	if err != nil {
		return nil, fmt.Errorf("failed to parsing mongo metadata,because of %v", err)
	}

	options := options.Client().ApplyURI(connStr)
	client, err := mongo.Connect(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("failed to establish connection with mongo,because of %v", err)
	}

	if err = client.Ping(ctx, readpref.Primary()); err != nil {
		return nil, fmt.Errorf("failed to ping mongo,because of %v", err)
	}

	return &mongoScaler{
		metadata: meta,
		client:   client,
	}, nil
}

func parseMongoMetadata(config *ScalerConfig) (*mongoMetadata, string, error) {
	var connStr string
	//setting default metadata
	meta := mongoMetadata{
		collection: defaultCollection,
		query:      "",
		queryValue: defaultQueryValue,
		dbName:     defaultDB,
	}

	//parse metaData from ScaledJob config
	if val, ok := config.TriggerMetadata["collection"]; ok {
		meta.collection = val
	} else {
		return nil, "", fmt.Errorf("no collection given")
	}

	if val, ok := config.TriggerMetadata["query"]; ok {
		meta.query = val
	} else {
		return nil, "", fmt.Errorf("no query given")
	}

	if val, ok := config.TriggerMetadata["queryValue"]; ok {
		queryValue, err := strconv.Atoi(val)
		if err != nil {
			return nil, "", fmt.Errorf("failed to convert %v to int,because of %v", queryValue , err.Error())
		}
		meta.queryValue = queryValue
	} else {
		return nil, "", fmt.Errorf("no queryValue given")
	}

	if val, ok := config.TriggerMetadata["dbName"]; ok {
		meta.dbName = val
	} else {
		return nil, "", fmt.Errorf("no dbName given")
	}

	// Resolve connectionString
	if c, ok := config.AuthParams["connectionString"]; ok {
		meta.connectionString = c
	} else if v, ok := config.TriggerMetadata["connectionStringFromEnv"]; ok {
		meta.connectionString = config.ResolvedEnv[v]
	} else {
		meta.connectionString = ""
		if val, ok := config.TriggerMetadata["host"]; ok {
			meta.host = val
		} else {
			return nil, "", fmt.Errorf("no host given")
		}
		if val, ok := config.TriggerMetadata["port"]; ok {
			meta.port = val
		} else {
			return nil, "", fmt.Errorf("no port given")
		}

		if val, ok := config.TriggerMetadata["username"]; ok {
			meta.username = val
		} else {
			return nil, "", fmt.Errorf("no username given")
		}
		//get password from env or authParams
		if v, ok := config.AuthParams["password"]; ok {
			meta.password = v
		} else if v, ok := config.TriggerMetadata["passwordFromEnv"]; ok {
			meta.password = config.ResolvedEnv[v]
		}

		if len(meta.password) == 0 {
			return nil, "", fmt.Errorf("no password given")
		}
	}

	if meta.connectionString != "" {
		connStr = meta.connectionString
	} else {
		// Build connection str
		addr := fmt.Sprintf("%s:%s", meta.host, meta.port)
		auth := fmt.Sprintf("%s:%s", meta.username, meta.password)
		connStr = "mongodb://" + auth + "@" + addr
	}

	return &meta, connStr, nil
}

func (s *mongoScaler) IsActive(ctx context.Context) (bool, error) {
	result, err := s.getQueryResult()
	if err != nil {
		mongoLog.Error(err, fmt.Sprintf("failed to get query result by mongo,because of %v", err))
		return false, err
	}
	return result > 0, nil
}

// Close disposes of mongo connections
func (s *mongoScaler) Close() error {
	if s.client != nil {
		err := s.client.Disconnect(context.TODO())
		if err != nil {
			mongoLog.Error(err, fmt.Sprintf("failed to close mongo connection,because of %v", err))
			return err
		}
	}

	return nil
}

// getQueryResult query mongo by meta.query
func (s *mongoScaler) getQueryResult() (int, error) {
	var (
		results []MongoFields
	)

	ctx, cancel := context.WithTimeout(context.Background(), mongoDefaultTimeOut)
	defer cancel()

	filter, err := json2BsonDoc(s.metadata.query)
	if err != nil {
		mongoLog.Error(err, fmt.Sprintf("failed to convert query param to bson.Doc,because of %v", err))
		return 0, err
	}

	cursor, err := s.client.Database(s.metadata.dbName).Collection(s.metadata.collection).Find(ctx, filter)
	if err != nil {
		mongoLog.Error(err, fmt.Sprintf("failed to query %v in %v,because of %v", s.metadata.dbName, s.metadata.collection, err))
		return 0, err
	}

	err = cursor.All(ctx, &results)
	if err != nil {
		mongoLog.Error(err, fmt.Sprintf("failed to traversal the query results,because of %v", err))
		return 0, err
	}

	return len(results), nil
}

// GetMetrics query from mongo,and return to external metrics
func (s *mongoScaler) GetMetrics(ctx context.Context, metricName string, metricSelector labels.Selector) ([]external_metrics.ExternalMetricValue, error) {
	num, err := s.getQueryResult()
	if err != nil {
		return []external_metrics.ExternalMetricValue{}, fmt.Errorf("failed to inspect momgo,because of %v", err)
	}

	metric := external_metrics.ExternalMetricValue{
		MetricName: metricName,
		Value:      *resource.NewQuantity(int64(num), resource.DecimalSI),
		Timestamp:  metav1.Now(),
	}

	return append([]external_metrics.ExternalMetricValue{}, metric), nil
}

// GetMetricSpecForScaling get the query value for scaling
func (s *mongoScaler) GetMetricSpecForScaling() []v2beta2.MetricSpec {
	targetQueryValue := resource.NewQuantity(int64(s.metadata.queryValue), resource.DecimalSI)
	metricName := kedautil.NormalizeString(fmt.Sprintf("%s-%s", "mongo", s.metadata.connectionString))

	externalMetric := &v2beta2.ExternalMetricSource{
		Metric: v2beta2.MetricIdentifier{
			Name: metricName,
		},
		Target: v2beta2.MetricTarget{
			Type:         v2beta2.AverageValueMetricType,
			AverageValue: targetQueryValue,
		},
	}
	metricSpec := v2beta2.MetricSpec{
		External: externalMetric, Type: externalMetricType,
	}
	return []v2beta2.MetricSpec{metricSpec}
}

// json2BsonDoc convert Json to Bson.Doc
func json2BsonDoc(js string) (doc bsonx.Doc, err error) {
	doc = bsonx.Doc{}
	err = bson.UnmarshalExtJSON([]byte(js), true, &doc)
	if err != nil {
		return nil, err
	}

	if len(doc) == 0 {
		return nil, errors.New("empty bson document")
	}

	return doc, nil
}
