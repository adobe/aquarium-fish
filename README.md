# Aquarium Fish

Distributed p2p system to manage resources. Primarily was developed to manage the dynamic Jenkins
CI agents in heterogeneous environment and simplify the infrastructure management, but can be used
in various applications to have self-management resources and simple REST API to manage p2p cluster.

Eventally becomes an internal cloud or pool of resources with high availability and business
continuity features - an essential part of the modern infrastructure in worldwide companies. It
will allow to build the automation without an issues of centralization (by proxying requests to
nearby services), complete control on the environments and safety provided by sandboxing and
dynamic nature of the envs.

Aquarium will make the resource management as simple as possible and hopefully unify the dynamic
resource management by integrating multiple environment providers (VM, container, native, clouds,
etc.) to one entry point of allocating devices which can be used across the organization.

## Requirements

To run the node you need nothing, but the drivers usually require some apps to be installed into
the environment.

## Goals

* Completely distributed system
* Run and operate locally with minimal required configuration
* Flexible to use different database engines
* Simple interface for the drivers which provides resources
* Proper sandboxing of the running resources (host only networking by default)
* Compact API with straightforward definitions
* Socks5 and other proxies to redirect the applications to nearby services

## Usage

To use the Aquarium Fish you just need to execute the next steps:
* Ensure the driver requirements for needed driver are installed
* Run Fish node
* Obtain the generated admin user token
* With HTTP API:
   * Create Label which describes the resource you want to see
   * Create Application to request the resource
   * Use the allocated resource
   * Destroy the resource

### To run locally

In order to test the Fish locally with just one node or multiple local nodes:
```
$ ./aquarium-fish
```

There is a number of options you can pass to the application, check `--help` to get them, but the
most important ones is:
* `--api` - is where the Fish API will listen, default is `0.0.0.0:8001` (it also is used for meta
so your VMs will be able to ask for the metadata)
* `--db` - is the listen interface for database sync. Exactly this address will be used by the other
nodes.
* `--cfg` - use the yaml config to specify the options

If you want to use the secondary node on the same host - provide a simple config with overridden
node name, because the first will use hostname as node name:
* local2.yml
   ```yaml
   ---
   node_name: test-2
   db_address: 127.0.0.1:9002
   api_address: 0.0.0.0:8002
   db_join:
     - 127.0.0.1:9001
   ```

```
$ ./aquarium-fish --cfg local2.yml
```

### To run as a cluster

Quite the same as running locally, but `--db` should be the actual ip/name endpoint of the host,
since it will be used to connect by the other nodes (so 0.0.0.0 will not work here). For example if
you can connect from outside to the host via `10.0.4.35` - you need to use `10.0.4.35:9001` here.

#### Security

By default Fish generates a simple CA and key/cert pair for Server & Client auth - it just shows the
example of cluster communication transport protection via TLS. If CA certificate is not exists - it
will be generated, if it exists - generation will be skipped. If node certificate and key are
exists, they will be used, but if not - Fish will try to generate them out of CA cert and key. So CA
key is not needed if you already generated the node certificate yourself.

TLS encryption is a must, so make sure you know how to generate CA certificate and control CA to
issue the node certificates. Today it's the most secure way to ensure noone will join your cluster
without your permission and do not intercept the API & DB communication. Separated CA is used to
check that the server (or client) is the one is approved.

Maybe in the future Fish will allow to manage the cluster CA and issue certificate for a new node,
but for now just check openssl and https://github.com/jcmoraisjr/simple-ca for reference.

### Cluster usage

To initialize cluster you need to create users with admin account and create labels you want to use.
In order to use the resources manager manually - check the `API` section and follow the next general
directions:

1. Get your user and it's token
2. Check the available labels on the cluster (and create some if you need them)
3. Create Application with description of what kind of resource you need
4. Check the Status of your application and wait for "ALLOCATED" status
5. Now resource is allocated, it's all yours and, probably, already pinged you
6. When you're done - request Application to deallocate the resource
7. Make sure the Application status is "DEALLOCATED"

To use with Jenkins - you can install [Aquarium Net Jenkins](https://github.com/adobe/aquarium-net-jenkins)
cloud plugin to dynamically allocate the required resources. Don't forget to add the served labels
to the cloud and you will be ready to go.

### Users policy

For now the policy is quite simple - `admin` user can do anything, regular users can just use the
cluster (create application, list their resources and so on). The applications & resources could
contain sensetive information (like jenkins agent secret), so user will see just the owned
applications and will be able to control only them.

## Implementation

Go was choosen due to it's go-dqlite, simple one-binary executable resources management with
embedded sql database that is synced automatically between the nodes. It's needed to store cluster
information for decision making (for example: which node will allocate the requested resources to
handle the workload in the most efficient way).

Resource drivers is the way nodes managing the resources. For example - if I have VMWare Fusion
installed on my machine - I can run Fish and it's VMX driver will automatically detect that it can
run VMX images. In case I have docker installed too - I can use both for different workloads or
select the ones I actually want to use by `--drivers` option or via the API.

### Internal DB structure

The cluster supports the internal SQL database, which provides a common storage for the cluster
info. The current schema could be found in OpenAPI format here:
 * When the Fish app is running locally: https://0.0.0.0:8001/api/
 * YAML specification: https://github.com/adobe/aquarium-fish/blob/master/docs/openapi.yaml

### How the cluster choose node for resource allocation

The cluster can't force any node to follow the majority decision, so the rules are providing full
consensus.

For now the rule is simple - when all the nodes are voted each node can find the first node in the
vote table that answered "yes". There is a couple of protection mechanisms like "CreateAt" to find
the actual first one and "Rand" field as last resort (if the other params are identical).

In the future to allow to update cluster with the new rules the Rules table will be created and the
different versions of the Aquarium Fish could find the common rules and switch them depends on
Application request. Rules will be able to lay on top of any information about the node.

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

## UI

Simplify the cluster management, for example adding labels or check the status.

**TODO**

## API

There is a number of ways to communicate with the Fish cluster, and the most important one is API.

You can use `curl`, for example, to do that:
```
$ curl -u "admin:YOUR_TOKEN" -X GET 127.0.0.1:8001/api/v1/label/
{"message": "Cluster resources list", "data": [{...}, ...]}
```

The current API could be found in OpenAPI format here:
 * When the Fish app is running locally: https://0.0.0.0:8001/api/
 * YAML specification: https://github.com/adobe/aquarium-fish/blob/master/docs/openapi.yaml
