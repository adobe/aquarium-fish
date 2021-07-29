#!/bin/sh -e
# Common build script - do not run directly
# Use ./build-linux.sh / ./build-macos.sh instead

[ "x$suffix" != "x" ] || suffix="$1"
[ "x$suffix" != "x" ] || suffix="$(uname -s)_$(uname -m)"

cd "${root_dir}"

./check.sh

echo "--- PATCH GO-DQLITE ---"
# Apply a small patch to the go-dqlite go package
gopath=$(go env GOPATH)
go get -d github.com/canonical/go-dqlite@v1.8.0
chmod -R u+w "$gopath/pkg/mod/github.com/canonical/go-dqlite@v1.8.0"
patch -N -p1 -d "$gopath/pkg/mod/github.com/canonical/go-dqlite@v1.8.0" < deps/go-dqlite.patch || true

echo "--- PATCH OAPI-CODEGEN ---"
# Generate the API code patch
go get -d "github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.8.1"
chmod -R u+w "$gopath/pkg/mod/github.com/deepmap/oapi-codegen@v1.8.1"
patch -N -p1 -d "$gopath/pkg/mod/github.com/deepmap/oapi-codegen@v1.8.1" < deps/oapi-codegen.patch || true

echo "--- GENERATE CODE FOR AQUARIUM-FISH ---"
find ./lib/ -name '*.gen.go' -delete
go generate -v ./lib/...

echo

echo "--- BUILD AQUARIUM-FISH ---"

if [ "x${RELEASE}" != "x" ]; then
    export GIN_MODE=release
else
    echo "--- WARNING: build DEBUG mode ---"
fi

export CGO_CFLAGS="${UV_CFLAGS} ${RAFT_CFLAGS} ${SQLITE_CFLAGS} ${DQLITE_CFLAGS}"
export CGO_LDFLAGS="${SET_CGO_LDFLAGS}"
go build -ldflags="-s -w" -a -o "aquarium-fish.$suffix" ./cmd/fish

# Remove debug symbols
strip "aquarium-fish.$suffix"
