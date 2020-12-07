# mongo with ScaledJob

## Trigger Specification
This specification describes the mongo trigger that scales based on result of Mongo query.

The trigger always requires the following information:
- dbName  -Name of the database
- collection -Name of the collection
- query -a Mongo query that should return single numeric value
- queryValue - a threshold that is used as targetAverageValue in HPA.

To provide information about how to connect to Mongo you can provide
- connectionStringFromEnv -Mongo connection string that should point to environment variable with valid value

Or provide more detailed information:
- host - The host of the Mongo server
- port - The port of the Mongo server
- username - Username to authenticate with to Mongo database
- passwordFromEnv - Password for the given user
## Authentication Parameters 
You can authenticate by using connection string or password authentication.

Connection String Authentication:
- connectionString  -Connection string for Mongo database

Password Authentication:
- password - Password for configured user to login to Mongo database variables.

## Example
Here is an example of how to deploy a scaled Job with the mongo scale trigger which uses TriggerAuthentication.
```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledJob
metadata:
  name: mongo-job
  namespace: kube-system
spec:
  jobTargetRef:
    template:
      spec:
        containers:
          - name: mongo-consumer
            image: registry-vpc.cn-hangzhou.aliyuncs.com/carsonow/mongo-consumer:v1.0
            imagePullPolicy: IfNotPresent
        restartPolicy: Never
    backoffLimit: 1
  pollingInterval: 30             # Optional. Default: 30 seconds
  maxReplicaCount: 30             # Optional. Default: 100
  successfulJobsHistoryLimit: 0   # Optional. Default: 100. How many completed jobs should be kept.
  failedJobsHistoryLimit: 10      # Optional. Default: 100. How many failed jobs should be kept.
  triggers:
    - type: mongo
      metadata:
        dbName: "local"
        collection: "trade"
        query: '{"region":"eu-1","state":"running","plan":"planA"}'
        queryValue: "1"
      authenticationRef:
        name: mongo-trigger
---
apiVersion: keda.sh/v1alpha1
kind: TriggerAuthentication
metadata:
  name: mongo-trigger
  namespace: kube-system
spec:
  secretTargetRef:
    - parameter: connectionString
      name: mongo-secret
      key: connect
---
apiVersion: v1
kind: Secret
metadata:
  name: mongo-secret
  namespace: kube-system
type: Opaque
data:
  connect: mongodb://root:oompig@37.107.117.212:27017
```





