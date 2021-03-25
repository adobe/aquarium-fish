# Aquarium: Fish

Distributed p2p system to manage resources. Primarily was developed in order to manage the dynamic
Jenkins CI and simplify the infrastructure management, but can be used in various applications to
have self-management resources and simple REST API to manage p2p cluster.

## Usage

### To run locally

In order to test the Fish locally with just one node or multiple local nodes:
```
$ ./aquarium-fish --api 0.0.0.0:8001 --db 127.0.0.1:9001
```

* `--api` - is where the Fish API will listen, usually it's `0.0.0.0:8001` (it also is used for meta
so your VMs will be able to ask for the metadata)
* `--db` - is the listen interface for database sync. Exactly this address will be used by the other
nodes.

If you want to use the secondary node on the same host - provide a simple config with overridden
node name, because the first will use hostname as node name:
* test.yml
   ```yaml
   ---
   node_name: test-node-1
   ```

```
$ ./aquarium-fish --api 0.0.0.0:8002 --db 127.0.0.1:9002 --cfg test.yml --join 127.0.0.1:9001
```

### To run in the real cluster

Quite the same as running locally, but `--db` should be the actual ip/name endpoint of the host,
since it will be used to connect by the other nodes (so 0.0.0.0 will not work here). For example if
you can connect from outside to the host via `10.0.4.35` - you need to use `10.0.4.35:9001` here.

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

To use with Jenkins - you can install [Aquarium Net Jenkins](https://git.corp.adobe.com/CI/aquarium-net-jenkins)
cloud plugin to dynamically allocate the required resources. Don't forget to add the served labels
to the cloud and you will be ready to go.

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
info.

* **Users** - contains limits and hash to login, id (login) is unique, `admin` created during the
first cluster start and prints it to stderr.
* **Nodes** - each node need to report it's description, status and ensure there is no duplications.
* **Labels** - this one filled by the cluster admin, depends on the needs. Labels could be
implemented in different drivers, but it's not recommended to keep the label stable. Version could
be used during request, but by default it's the latest.
* **Applications** - is a resource request created by the user. Each node votes for the availability
to allocate the resource and the cluster choose which one node will actually do the work.
* **Votes** - when Application becomes available for the node it starts to vote to notify the
cluster about its availability. Votes are basically "yes" or "no" and could take a number of rounds
depends on the cluster voting and election rules.
* **Resources** - list of the active resources to be able to properly restore the state during the
cluster node restart. Also contains additional info about the resource, for example user requested
metadata, which is available for the resource through the `Meta API`.

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
$ curl -u "admin:YOUR_TOKEN" -X GET 127.0.0.1:8001/api/v1/resource/
{"message": "Cluster resources list", "data": [{...}, ...]}
```

### General API

Route: `/api/v1/`

Requires HTTP Basic (or JWT auth from UI in the future) auth of existing user to execute any request
Basically uses GET (get data), POST (create/update data), DELETE (remove data) requests.

#### Users

Route: `/api/v1/user/`

Allow to get the info about the cluster users, add new, modify or remove them.

#### Nodes

Route: `/api/v1/node/`

Allows to get info about the cluster nodes and allows to manipulate each node of the cluster
personally.

#### Applications

Route: `/api/v1/application/`

Resource application in order to allocate resources. Can be created and listed, but not updated or
deleted.

* `/api/v1/application/:id/status` - Current status of the application
* `/api/v1/application/:id/resource` - Linked resource when some node took it in execution
* `/api/v1/application/:id/deallocate` - Execute it to deallocate the application resource

#### Resources

Route: `/api/v1/resource/`

Allow to get info about the allocated cluster resources.

#### Labels

Route: `/api/v1/label/`

Allow to get info about the cluster labels, create the new and delete the existing ones.

Label - is one of the most important part of the system, because it makes the resources reproducible
in time. It contains the driver name and configuration, so can be started again and again as much
times we need. Versions make possible to update the labels and store the old ones in case we need to
run the same environment 10y from now and rebuild the old code revision for example.

Labels can't be updated. Once they are stored - they are here to keep the history of environements
and make possible to mark build with the specified label version in order to be able to reproduce it
later. Also labels can be implemented just by one driver. If you want to use another one - you will
need to create another label version and the resource requests that uses latest will swith to it.

### Meta API

Route: `/meta/v1/`

In order to provide additional info for the resource environment there is `Meta API` which is
available for the controlled networks. If the request is coming from such network - Meta API checks
the resource table trying to locate the required resource by IP or HW address and gives the
requestor required information based on this data.

Common params:
* `format=` - can be used to format the output:
   * json - default format
   * yaml - in case someone need yaml
   * env - useful for shell scripts to get the quoted key=value env variables
      * `prefix=` - allows to set a custom prefix for the env variables

#### Data

Route: `/meta/v1/data/`

Requests the stored metadata for the current resource. For example metadata could contain Jenkins
URL and JNLP Agent token to get the Agent and connect to the Agent node.
