#!/bin/sh -e
# Common build script - do not run directly
# Use ./build-linux.sh / ./build-macos.sh instead

[ "x$suffix" != "x" ] || suffix="$1"
[ "x$suffix" != "x" ] || suffix="$(uname -s)_$(uname -m)"

cd "${root_dir}"

export CGO_CFLAGS="${UV_CFLAGS} ${RAFT_CFLAGS} ${SQLITE_CFLAGS} ${DQLITE_CFLAGS}"

if [ "x${RELEASE}" != "x" ]; then
    export GIN_MODE=release
else
    echo "--- WARNING: build DEBUG mode ---"
fi

echo "--- BUILD AQUARIUM-FISH ---"
go build -ldflags="-s -w" -a -o "aquarium-fish.$suffix" ./cmd/fish
