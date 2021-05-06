#!/bin/sh -e
# Build fish for linux (using docker)

root_dir=$(dirname "`realpath "$0"`")
module=$(grep '^module' "${root_dir}/go.mod" | cut -d ' ' -f 2)

# Run in docker container
if [ -z "${GOOS}" ]; then
    docker run --rm -it -v "$root_dir":/go/src/${module}:z -w /go/src/${module} -e GOOS=linux -e GOARCH=amd64 golang:1.16 ./build-linux.sh
    exit 0
fi

export DEBIAN_FRONTEND=noninteractive

apt update
apt install -y autotools-dev autoconf patch libtool m4 automake

deps_dir=${root_dir}/deps
. deps/build-deps.sh

export CGO_LDFLAGS="-static -pthread ${DQLITE_LIBS} ${SQLITE_LIBS} ${RAFT_LIBS} ${UV_LIBS} -lm -ldl"

cd "${root_dir}"
sh _build.sh
