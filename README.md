# Aquarium: Fish

Distributed p2p system to manage resources. Primarily was developed in order to manage the dynamic
Jenkins CI and simplify the infrastructure management, but can be used in various applications to
have self-management resources and simple REST API to manage p2p cluster.

## Requirements

## Usage

In order to use with Jenkins - you can install [Aquarium Net Jenkins](https://git.corp.adobe.com/CI/aquarium-net-jenkins)
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

## Internal DB structure

The cluster supports the internal SQL database, which provides a common storage for the cluster
info.

* **Users** - contains limits and hash to login, id (login) is unique, `admin` created during the
first cluster start and prints it to stderr.
* **Nodes** - each node need to report it's description, status and ensure there is no duplications.
* **Labels** - this one filled by the cluster admin, depends on the needs. Labels could be
implemented in different drivers, but it's not recommended to keep the label stable. Version could
be used during request, but by default it's the latest.
* **Resources** - list of the active resources to be able to properly restore the state during the
cluster node restart. Also contains additional info about the resource, for example user requested
metadata, which is available for the resource through the `Meta API`.

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

#### Resources

Route: `/api/v1/resource/`

Allow to get info about the cluster resources, request the new and delete the existing ones.

#### Labels

Route: `/api/v1/label/`

Allow to get info about the cluster labels, create the new and delete the existing ones.

### Meta API

Route: `/meta/v1/`

In order to provide additional info for the resource environment there is `Meta API` which is
available for the controlled networks. If the request is coming from such network - Meta API checks
the resource table trying to locate the required resource by IP or HW address and gives the
requestor required information based on this data.

#### Data

Route: `/meta/v1/data/`

Requests the stored metadata for the current resource. For example metadata could contain Jenkins
URL and JNLP Agent token to get the Agent and connect to the Agent node.
