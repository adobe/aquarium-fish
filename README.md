# [Aquarium Fish](https://github.com/adobe/aquarium-fish)

[![CI](https://github.com/adobe/aquarium-fish/actions/workflows/main.yml/badge.svg?branch=main)](https://github.com/adobe/aquarium-fish/actions/workflows/main.yml)

Main part of the [Aquarium](https://github.com/adobe/aquarium-fish/wiki/Aquarium) distributed p2p
system to manage resources. Primarily was developed to manage the dynamic Jenkins CI agents in
heterogeneous environment and simplify the infrastructure management, but can be used in various
applications to have self-management resources with simple gRPC/Connect API to operate p2p cluster.

Eventually becomes an internal cloud or pool of resources with high availability and business
continuity features - an essential part of the modern infrastructure in international companies. It
will allow to build the automation without the issues of centralization (by proxying requests to
nearby services), complete control of the environments and security provided by sandboxing and
dynamic nature of the envs.

The Aquarium system will make the resource management as simple as possible and will unify the
dynamic resource management by integrating multiple environment providers (VM, container, native,
clouds, etc.) to one entry point of allocating devices which can be used across the organization.

## Requirements

In general it can be built and used on any OS/architecture but for now the primary ones are:
* MacOS
* Linux

To run the Node you need nothing, but the drivers usually require some apps to be installed into
the environment.

## Goals

* Completely distributed system
* Run and operate locally with minimal required configuration
* Flexible to use different database engines (currently using bitcask for high performance)
* Simple interface for the drivers which provides resources
* Proper sandboxing of the running resources (host only networking by default)
* Modern gRPC/Connect API with Protocol Buffers definitions
* Role-Based Access Control (RBAC) for fine-grained permissions
* Socks5 and other proxies to redirect the applications to nearby services

## Usage

To use the Aquarium Fish you just need to execute the next steps:
* Ensure the dependencies for needed driver are installed
* Run Fish node
* Obtain the generated admin user token
* With gRPC/Connect API:
   * Create Label which describes the resource you want to see
   * Create Application to request the resource
   * Use the allocated resource
   * Destroy the resource when the job is done

### To run locally

In order to test the Fish locally with just one node or multiple local nodes:
```
$ ./aquarium-fish
```

There is a number of options you can pass to the application, check `--help` to get them, but the
most important ones is:
* `--api` - is where the Fish API will listen, default is `0.0.0.0:8001` (it also is used for meta
so your VMs will be able to ask for the metadata)
* `--cfg` - use the yaml config to specify the options

If you want to use the secondary node on the same host - provide a simple config with overridden
node name, because the first will use hostname as node name:
* local2.yml
   ```yaml
   ---
   node_name: test-2
   api_address: 0.0.0.0:8002
   ```

```
$ ./aquarium-fish --cfg local2.yml
```

#### Security

By default Fish generates a simple CA and key/cert pair for Server & Client auth - it just shows
the example of cluster communication transport protection via TLS and uses certificate public key
as identifier of the cluster node. If a CA certificate is not exists - it will be generated. If
node certificate and key are exists, they will be used, but if not - Fish will try to generate them
out of CA cert and key. So CA key is not needed for the node if you already generated the node
certificate yourself.

TLS encryption is a must, so make sure you know how to generate a CA certificate and control CA to
issue the node certificates. Today it's the most secure way to ensure noone will join your cluster
without your permission and do not intercept the API & sync communication. Separated CA is used to
check that the server (or client) is the one is approved in the cluster.

Maybe in the future Fish will allow to manage the cluster CA and issue certificate for a new node,
but for now just check openssl and https://github.com/jcmoraisjr/simple-ca for reference.

#### Performance

It really depends on how you want to run the Fish node, in general there are 2 cases:

1. **To serve local resources of the machine**: so you run it on the performant node and don't want
to consume too much of it's precious resources or interfere somehow: then you can use -cpu and -mem
params to limit the node in CPU and RAM utilization. Of course that will impact the API processing
performance, but probably you don't need much since you running a cluster and can distribute the
load across the nodes. You can expect that with 2 vCPU and 512MB of ram it could process ~16 API
requests per second.
2. **To serve remote/cloud resources**: It's worth to set the target on RAM by -mem option, but not
much to CPU. The RAM limit will help you to not get into OOM - just leave ~2GB of RAM for GC and
you will get the maximum performance. With 16 vCPU Fish can serve ~50 API requests per second.

Most of the time during API request processing is wasted on user password validation, so if you
need to squeeze more rps from Fish node you can lower the Argon2id parameters in crypt.go, but with
that you need to make sure you understand the consequences:
https://datatracker.ietf.org/doc/html/draft-irtf-cfrg-argon2-04#section-4

### To run as a cluster

**TODO [#30](https://github.com/adobe/aquarium-fish/issues/30):** This functionality is in active
development, the available logic can't handle the cluster.

Just make sure there is a path from one node to at least one another - there is no requirement of
seeing the entire cluster for each node, but it need to be able to connect to at least one. More
visibility is better up to 8 total - because it's the default limit of cluster connections for the
node.

#### Cluster usage

To initialize cluster you need to create users with admin account and create Labels you want to
use. In order to use the resources manager manually - check the `API` section and follow the next
general directions:

1. Get your user and it's token
2. Check the available Labels on the cluster (and create some if you need them)
3. Create Application with description of what kind of resource you need
4. Check the Status of your Application and wait for "ALLOCATED" status
5. Now resource is allocated, it's all yours and, probably, already pinged you
6. When you're done - request Application to deallocate the resource
7. Make sure the Application status is "DEALLOCATED"

To use with Jenkins - you can install [Aquarium Net Jenkins](https://github.com/adobe/aquarium-net-jenkins)
cloud plugin to dynamically allocate the required resources. Don't forget to add the served Labels
to the cluster and you will be ready to go.

### Users and RBAC

Fish now includes a comprehensive Role-Based Access Control (RBAC) system with three default roles:

#### Default Roles

* **Administrator**: Full access to all system resources and operations
  - Can manage users, roles, labels, and applications
  - Can view and control all cluster nodes
  - Has administrative privileges for system maintenance

* **User**: Standard user with limited permissions
  - Can create and manage their own applications
  - Can view available labels
  - Cannot access other users' resources or system administration

* **Power**: Additional for the **User** role to have:
  - Can create and manage application tasks
  - Can access SSH proxy resources

#### Permission System

The RBAC system is defined through Protocol Buffers and automatically generates permissions based
on gRPC service methods. Each role is granted specific permissions for resources and actions:

- **Resources**: Services like `ApplicationService`, `LabelService`, `UserService`, etc.
- **Actions**: Methods like `Create`, `Get`, `List`, `Update`, `Delete`
- **Additional Actions**: Special permissions like `GetAll`, `UpdateAll` for cross-user operations

For advanced setups, you can create custom roles with specific permission combinations using the
Role management API.

### Monitoring

Aquarium-Fish supports full OpenTelemetry spectrum: Metrics, Logging, Tracing and Profiling. It can
be paired with remote OTLP GRPC telemetry receiver or store the telemetry locally with an ability
to sync it later with the remote using tool `tools/otel-import-file/otel-import-file.go`.

The monitoring could be enabled in Fish configuration by specifying the next block:
```yaml
monitoring:
  enabled: false  # General killswitch, set it true to enable monitoring
  enable_logs: true
  enable_metrics: true
  enable_profiling: true
  enable_tracing: true

  otlp_endpoint: ""  # You can specify the OTLP GRPC remote here (ex. "localhost:4317")
  pyroscope_endpoint: ""  # Since OTLP profiling is still experimental (ex. "http://localhost:4040")
  file_export_path: ""  # Where to store the telemetry files, will be used if `otlp_endpoint` unset

  sample_rate: 1.0  # 0.0-1.0 rate to reduce the amount of traffic
  metrics_interval: "15s"  # How often to take measurements
  profiling_interval: "30s"  # How often to capture profiling info
```

Good example of the server you can find in https://github.com/grafana/docker-otel-lgtm - it
integrates grafana as UI and prometheus, tempo, loki, pyroscope as DB backend. You can run it as:
```sh
$ docker run --name lgtm -p 3000:3000 -p 4317:4317 -p 4040:4040 --rm -ti -e GF_PATHS_DATA=/data/grafana grafana/otel-lgtm
```

## Implementation

Go was initially chosen because of go-dqlite, but became quite useful and modern way of making a
self-sufficient one-executable service which can cover multiple areas without performance sacrifice.
The way it manages dependencies and subroutines, structures logic makes it much better than python
for such purpose. Eventually we've moved away from dqlite and now use bitcask for high-performance
embedded storage that provides excellent read/write performance and data durability.

### API Architecture

Fish now uses a modern gRPC-first approach with Connect-RPC for maximum compatibility:

* **Protocol Buffers**: All API definitions are in `proto/aquarium/v2/` directory
* **gRPC**: Native gRPC support for high-performance applications
* **Connect-RPC**: HTTP/1.1 and HTTP/2 compatible REST-like API
* **JSON/YAML**: Support for both binary protobuf and JSON/YAML formats
* **Authentication**: HTTP Basic Auth with secure token-based system
* **RBAC**: Fine-grained permissions controlled by role assignments

### Database

Fish uses bitcask as the embedded database engine, providing:

* **High Performance**: Optimized for high read/write throughput
* **Durability**: Write-ahead logging ensures data consistency
* **Compaction**: Automatic garbage collection and space reclamation
* **Simplicity**: No external dependencies, single binary deployment
* **Crash Recovery**: Automatic recovery from unexpected shutdowns

The database automatically handles cleanup of completed applications and periodic compaction to maintain optimal performance.

### Drivers

Fish uses 2 driver types - `provider` and `gate`:
* Provider Driver - manages AppplicationResources from multiple providers (VMX, AWS, ...). It Has
  no access to Fish core, self-managing and only called by Fish to execute necessary operations to
  allocate or deallocate ApplicationResources and manipulate them.
* Gate Driver - allows to ingest external requests and uses Fish core to store data in Fish DB and
  request allocation/deallocation or somehow else provide access to the custom interfaces.

In the event you need to use more than one configuration for a given driver, you can add a suffix
`/<name>`. For example, `aws` and `aws/dev` will both utilize the AWS driver, but use a different
configuration. In this example, Labels created will need to specify either `driver: aws` or
`driver: aws/dev` to select which configuration to run.

### How the cluster choose node for resource allocation

The cluster can't force any node to follow the majority decision, so the rules are providing full
consensus.

For now the rule is simple - when all the nodes are voted, each node can find the first node in the
vote table that answered "yes". There are a couple of protection mechanisms like "CreateAt" to find
the actual first one and "Rand" field as a last resort (if the other params are identical).

In the future to allow to update cluster with the new rules the Rules table will be created and the
different versions of the Aquarium Fish could find the common rules and switch them depends on
Application request. Rules will be able to lay on top of any information about the node [#15](https://github.com/adobe/aquarium-fish/issues/15).

The election process:
* Once per 5 seconds the node checks the voting process
* If there is Application with status NEW:
   * If no Node Vote for the Application exists
      * Fish creates Vote depends on the current status of the Node and round of the election
   * If all the active cluster Nodes are voted
      * If there is "Yes" Votes
         * Application Election Rule applied to the votes
         * If the current Node is elected
            * If current Node is not executing Application already
               * Set Application status to ELECTED
               * Run the allocate process
         * Else if the current Node is not elected
            * If Application has no NEW status
               * Remove Vote and forget about the Application
            * Else if Vote round timeout is passed
               * Decide the elected node was not took the Application
               * execute next round vote
      * If there is no "Yes" Votes
         * If Vote round delay is passed
            * Increment Vote round and vote again on the current Node status

## Web UI Dashboard

Aquarium-Fish now have a nice Web-UI which allows users to quickly spin-up new environment and even
connect to it using ProxySSH. It's static single-page application (SPA) based on React-Router v7
which heavily utilizes server streaming to quickly share the latest DB updates and to reduce load
on the server side.

Web Dashboard could be visited on primary endpoint of the Fish right after start: https://localhost:8001/
and will allow all the users to login with their user/password.

## Development

Is relatively easy - you change logic, you run `./build.sh` to create a binary, testing it and send
the PR when you think it's perfect enough. That will be great if you can ask in the discussions or
create an issue on GitHub to align with the current direction and the plans.

### Linting

Fish uses golangci-lint to execute a huge number of static checks and you can run it locally like:
```sh
$ golangci-lint run -v
```

It uses the configuration from .golangci.yml file.

### Integration tests

The integration tests needs aquarium-fish* binary, so prior to execution please run `./build.sh`.

* To verify that everything works as expected you can run integration tests like that:
   ```sh
   $ go test -json -v -parallel 4 -count=1 -skip '_stress$' -race ./tests/... | go run ./tools/go-test-formatter/go-test-formatter.go -stdout_timestamp test -stdout_color -stdout_filter failed
   ```
* To run just one test of the suite on specific aquarium-fish binary:
   ```sh
   $ FISH_PATH=$PWD/aquarium-fish.darwin_amd64 go test -v -failfast -count 1 -run '^TEST_NAME$' ./tests
   ```
* To run the tests with monitoring - you can use `FISH_MONITORING` env variable. Set it to empty
  value if you want to store telemetry in the workspace as files or specify localhost to connect to
  the local OTLP/Pyroscope service (in docker container for example):
   ```sh
   $ FISH_MONITORING=localhost go test -json -v -parallel 1 -count=1 -skip '_stress$' -race ./tests/...
   ```
   ```

#### Docker & WEB UI testing

There are a couple of helpers to run specific tests:
* Mac->Lin build & integration tests runner: `./scripts/test-docker.sh`
* Web UI integration tests runner: `./scripts/webtest-docker.sh`

Both of them will build the linux binary for testing and you can disable that by `NOBUILD=1` env var,
also any argument given to those scripts will be passed to go test. By default the scripts will use
`./tests/...` and `./webtests/...` respectively.

In order to create tests - you can run local Fish node and use chromium "playwright-crx" extension to
figure out what kind of locators to use. It's mostly for js/python but allows to find the right
functions to look in the https://pkg.go.dev/github.com/playwright-community/playwright-go after that.

### Benchmarks

Fish contains a few benchmarks to make sure the performance of the node & cluster will be stable.
You can run them locally like that:
```sh
$ go test -bench . -benchmem '-run=^#' -cpu 1,2 ./...
goos: darwin
goarch: amd64
pkg: github.com/adobe/aquarium-fish/lib/crypt
cpu: Intel(R) Core(TM) i9-9880H CPU @ 2.30GHz
Benchmark_hash_new         	      20	  65924472 ns/op	67122440 B/op	     180 allocs/op
Benchmark_hash_new-2       	      33	  34709165 ns/op	67122834 B/op	     181 allocs/op
Benchmark_hash_isequal     	      33	  64242662 ns/op	67122424 B/op	     179 allocs/op
Benchmark_hash_isequal-2   	      32	  34741325 ns/op	67122526 B/op	     179 allocs/op
```

CI stores the previous results in branch gh-pages in json format. Unfortunately GitHub actions
workers perfromance is not stable, so it's recommended to execute the benchmarks on standaline.

### Direct Profiling

Standard go pprof profiling is enabled only in debug builds. It's available on unauthorized
endpoint https://localhost:8001/debug/pprof/ - so please don't use in production.

You can reach pprof data like that:
```
$ go tool pprof 'https+insecure://localhost:8001/debug/pprof/heap'
$ curl -ku "<USER>:<TOKEN>" 'https://localhost:8001/debug/pprof/heap?debug=1'
```

Or you can open https://localhost:8001/debug/pprof/ in browser to see the index.

## API

The API is built using modern gRPC and Connect-RPC protocols with Protocol Buffers for maximum performance and compatibility.

### Protocol Support

Fish supports multiple protocols for different use cases:

* **gRPC**: High-performance binary protocol for production applications
* **Connect-RPC**: HTTP/1.1 and HTTP/2 compatible with REST-like semantics
* **JSON/YAML**: Human-readable formats for debugging and curl usage

### API Endpoints

The API is served on `/grpc/` prefix and supports all services defined in the Protocol Buffer
specifications:

* **ApplicationService**: Manage applications and their lifecycle
* **LabelService**: Define and manage resource labels
* **UserService**: User management and authentication
* **RoleService**: RBAC role management
* **NodeService**: Cluster node information and maintenance

### Authentication

All API calls require HTTP Basic Authentication:
```bash
$ curl -u "admin:YOUR_TOKEN" -X POST "https://127.0.0.1:8001/grpc/aquarium.v2.LabelService/List" \
  -H "Content-Type: application/json" \
  -d '{}'
```

For gRPC clients, use appropriate authentication interceptors with basic auth credentials.

### API Documentation

* **Protocol Buffer Definitions**: `proto/aquarium/v2/` - Complete API specifications
* **Generated Code**: `lib/rpc/proto/aquarium/v2/` - Go client and server code
* **Connect Clients**: `lib/rpc/proto/aquarium/v2/aquariumv2connect/` - HTTP/gRPC client interfaces

### Example Usage

Using Connect-RPC with curl:
```bash
# List all labels
$ curl -u "admin:YOUR_TOKEN" -X POST "https://127.0.0.1:8001/grpc/aquarium.v2.LabelService/List" \
  -H "Content-Type: application/json" \
  -d '{}'

# Create an application
$ curl -u "admin:YOUR_TOKEN" -X POST "https://127.0.0.1:8001/grpc/aquarium.v2.ApplicationService/Create" \
  -H "Content-Type: application/json" \
  -d '{"application": {"label_uid": "your-label-uid"}}'
```

Also check `examples` and `tests` folder to get more info about the typical API usage.
