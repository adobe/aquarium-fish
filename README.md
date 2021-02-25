# Aquarium: Fish

Distributed p2p system to manage resources. Primarily was developed in order to manage the dynamic
Jenkins CI and simplify the infrastructure management, but can be used in various applications to
have self-management resources and simple REST API to manage p2p cluster.

## Requirements

## Usage

In order to use with Jenkins - you can use `Aquarium: Rod Jenkins` cloud plugin to dynamically
allocate the required resources.

## Implementation

Go was choosen due to it's go-dqlite, simple one-binary executable resources management with
embedded sql database that is synced automatically between the nodes. It's needed to store cluster
information for decision making (for example: which node will allocate the requested resources to
handle the workload in the most efficient way).

Resource drivers is the way nodes managing the resources. For example - if I have VMWare Fusion
installed on my machine - I can run Fish and it's VMX driver will automatically detect that it can
run VMX images. In case I have docker installed too - I can use both for different workloads or
select the ones I actually want to use by `--drivers` option or via the API.
