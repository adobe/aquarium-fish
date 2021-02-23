#!/bin/sh -e

root_dir=$(dirname "`realpath "$0"`")
module=$(grep '^module' "${root_dir}/go.mod" | cut -d ' ' -f 2)

# Run in docker container
if [ -z "${GOOS}" ]; then
    docker run --rm -it -v "$root_dir":/go/src/${module}:z -w /go/src/${module} -e GOOS=linux -e GOARCH=amd64 ubuntu:20.04 ./build-linux.sh
    exit 0
fi

export DEBIAN_FRONTEND=noninteractive

apt update
apt install -y --no-install-recommends software-properties-common
add-apt-repository -y ppa:dqlite/stable
apt install -y libdqlite-dev golang

export CGO_LDFLAGS="-v -static -pthread -ldqlite -lraft -luv -lsqlite3 -lm -ldl"
go build -ldflags="-v -s -w" -a -o aquarium-fish.linux ./cmd/fish
