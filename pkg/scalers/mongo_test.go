package scalers

import (
	"go.mongodb.org/mongo-driver/mongo"
	"testing"
)

var testMongoResolvedEnv = map[string]string{
	"Mongo_CONN_STR": "test_conn_str",
	"Mongo_PASSWORD": "test",
}

type parseMongoMetadataTestData struct {
	metadata    map[string]string
	resolvedEnv map[string]string
	raisesError bool
}

type mongoMetricIdentifier struct {
	metadataTestData *parseMongoMetadataTestData
	name             string
}

var testMONGOMetadata = []parseMongoMetadataTestData{
	// No metadata
	{
		metadata:    map[string]string{},
		resolvedEnv: testMongoResolvedEnv,
		raisesError: true,
	},
	// connectionStringFromEnv
	{
		metadata:    map[string]string{"query": `{"name":"John"}`, "collection": "demo", "queryValue": "12", "connectionStringFromEnv": "Mongo_CONN_STR", "dbName": "test"},
		resolvedEnv: testMongoResolvedEnv,
		raisesError: false,
	},
}

var mongoMetricIdentifiers = []mongoMetricIdentifier{
	{metadataTestData: &testMONGOMetadata[1], name: "mongo-test_conn_str"},
}

func TestParseMongoMetadata(t *testing.T) {
	for _, testData := range testMONGOMetadata {
		_, _, err := parseMongoMetadata(&ScalerConfig{TriggerMetadata: testData.metadata})
		if err != nil && !testData.raisesError {
			t.Error("Expected success but got error:", err)
		}
		if err == nil && testData.raisesError {
			t.Error("Expected error but got success")
		}
	}
}

func TestMongoGetMetricSpecForScaling(t *testing.T) {
	for _, testData := range mongoMetricIdentifiers {
		meta, _, err := parseMongoMetadata(&ScalerConfig{ResolvedEnv: testData.metadataTestData.resolvedEnv, TriggerMetadata: testData.metadataTestData.metadata})
		if err != nil {
			t.Fatal("Could not parse metadata:", err)
		}
		mockMongoScaler := mongoScaler{meta, &mongo.Client{}}

		metricSpec := mockMongoScaler.GetMetricSpecForScaling()
		metricName := metricSpec[0].External.Metric.Name
		if metricName != testData.name {
			t.Error("Wrong External metric source name:", metricName)
		}
	}
}

func TestJson2BsonDoc(t *testing.T) {
	var testJson = `{"name":"carson"}`
	doc, err := json2BsonDoc(testJson)
	if err != nil {
		t.Error("convert testJson to Bson.Doc err:", err)
	}
	if doc == nil {
		t.Error("the doc is nil")
	}
}
