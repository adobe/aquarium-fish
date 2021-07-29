#!/bin/sh -e
# Build fish for linux (using docker)

root_dir=$(dirname "`realpath "$0"`")
module=$(grep '^module' "${root_dir}/go.mod" | cut -d ' ' -f 2)

# Run in docker container
if [ "$(command -v go)" = "" -o "$(go env GOOS)" != "linux" ]; then
    # golang:1.16
    # WARN: Use only image ID, not tags
    # Login:
    #   $ docker login docker-hub-remote.dr-uw2.adobeitc.com
    #   username: <login>
    #   password: <API key from https://artifactory-uw2.adobeitc.com/artifactory/webapp/#/profile>
    docker run --rm -it -v "$root_dir":/go/src/${module}:z -w /go/src/${module} -e GOOS=linux -e GOARCH=amd64 docker-hub-remote.dr-uw2.adobeitc.com/golang@sha256:be0e3a0f3ffa448b0bcbb9019edca692b8278407a44dc138c60e6f12f0218f87 ./build-linux.sh
    exit 0
fi

export DEBIAN_FRONTEND=noninteractive

apt update
apt install -y autotools-dev autoconf patch libtool m4 automake

deps_dir=${root_dir}/deps
. deps/build-deps.sh

export SET_CGO_LDFLAGS="-static -pthread ${DQLITE_LIBS} ${SQLITE_LIBS} ${RAFT_LIBS} ${UV_LIBS} -lm -ldl"

cd "${root_dir}"
sh _build.sh
