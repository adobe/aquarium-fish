#!/bin/sh -e

root_dir=$(dirname "`realpath "$0"`")

cd "${root_dir}"
mkdir -p deps

if [ ! -d deps/libuv-v1.41.0 ]; then
    curl -sLo deps/libuv-v1.41.0.tar.gz https://dist.libuv.org/dist/v1.41.0/libuv-v1.41.0.tar.gz
    tar xf deps/libuv-v1.41.0.tar.gz -C deps
    cd deps/libuv-v1.41.0

    sh autogen.sh
    ./configure
    make -j8
fi

export UV_CFLAGS="-I${root_dir}/deps/libuv-v1.41.0/include"
export UV_LIBS="-L${root_dir}/deps/libuv-v1.41.0/.libs -luv"

cd "${root_dir}"
if [ ! -d deps/raft-0.9.25 ]; then
    curl -sLo deps/raft-v0.9.25.tar.gz https://github.com/canonical/raft/archive/v0.9.25.tar.gz
    tar xf deps/raft-v0.9.25.tar.gz -C deps
    cd deps/raft-0.9.25
    patch -p1 < ../raft.patch

    autoreconf -i
    ./configure
    make -j8
fi

export RAFT_CFLAGS="-I${root_dir}/deps/raft-0.9.25/include"
export RAFT_LIBS="-L${root_dir}/deps/raft-0.9.25/.libs -lraft"

cd "${root_dir}"
if [ ! -d deps/sqlite-autoconf-3340100 ]; then
    curl -sLo deps/sqlite-autoconf-3340100.tar.gz https://www.sqlite.org/2021/sqlite-autoconf-3340100.tar.gz
    tar xf deps/sqlite-autoconf-3340100.tar.gz -C deps
    cd deps/sqlite-autoconf-3340100

    ./configure
    make -j8
fi

export SQLITE_CFLAGS="-I${root_dir}/deps/sqlite-autoconf-3340100"
export SQLITE_LIBS="-L${root_dir}/deps/sqlite-autoconf-3340100/.libs -lsqlite3"

cd "${root_dir}"
if [ ! -d deps/dqlite-1.6.0 ]; then
    curl -sLo deps/dqlite-v1.6.0.tar.gz https://github.com/canonical/dqlite/archive/v1.6.0.tar.gz
    tar xf deps/dqlite-v1.6.0.tar.gz -C deps
    cd deps/dqlite-1.6.0
    patch -p1 < ../dqlite.patch

    autoreconf -i
    ./configure
    make -j8
fi

export DQLITE_CFLAGS="-I${root_dir}/deps/dqlite-1.6.0/include"
export DQLITE_LIBS="-L${root_dir}/deps/dqlite-1.6.0/.libs -ldqlite"

cd "${root_dir}"

# Apply a small patch to the go-dqlite go package
go get -d github.com/canonical/go-dqlite@v1.8.0
chmod -R u+w ~/go/pkg/mod/github.com/canonical/go-dqlite@v1.8.0
patch -N -p1 -d ~/go/pkg/mod/github.com/canonical/go-dqlite@v1.8.0 < deps/go-dqlite.patch || true

export CGO_CFLAGS="${UV_CFLAGS} ${RAFT_CFLAGS} ${SQLITE_CFLAGS} ${DQLITE_CFLAGS}"
# MacOS doesn't support static executables, so using .a to compile-in the dependencies at least
export CGO_LDFLAGS="-v ${root_dir}/deps/dqlite-1.6.0/.libs/libdqlite.a ${root_dir}/deps/sqlite-autoconf-3340100/.libs/libsqlite3.a ${root_dir}/deps/raft-0.9.25/.libs/libraft.a ${root_dir}/deps/libuv-v1.41.0/.libs/libuv.a"
GOOS=darwin go build -ldflags="-v -s -w" -a -o aquarium-fish.darwin ./cmd/fish

# Sign the binary to remove the sandbox restrictions
codesign -s - -f --entitlements macos.plist aquarium-fish.darwin
