#!/bin/sh -e

UV_VERSION=1.41.0
RAFT_VERSION=0.9.25
SQLITE_VERSION=3340100
DQLITE_VERSION=1.6.0


[ "x$suffix" != "x" ] || suffix="$1"
[ "x$suffix" != "x" ] || suffix="$(uname -s)_$(uname -m)"

[ "x$deps_dir" != "x" ] || deps_dir=$(dirname "`realpath "$0"`")
cd "${deps_dir}"

echo "--- DOWNLOAD DEPS ---"
[ -f libuv-${UV_VERSION}.tar.gz ] || curl -sLo libuv-${UV_VERSION}.tar.gz \
    "https://dist.libuv.org/dist/v${UV_VERSION}/libuv-v${UV_VERSION}.tar.gz"
[ -f raft-${RAFT_VERSION}.tar.gz ] || curl -sLo raft-${RAFT_VERSION}.tar.gz \
    "https://github.com/canonical/raft/archive/v${RAFT_VERSION}.tar.gz"
[ -f sqlite-${SQLITE_VERSION}.tar.gz ] || curl -sLo sqlite-${SQLITE_VERSION}.tar.gz \
    "https://www.sqlite.org/2021/sqlite-autoconf-${SQLITE_VERSION}.tar.gz"
[ -f dqlite-${DQLITE_VERSION}.tar.gz ] || curl -sLo dqlite-${DQLITE_VERSION}.tar.gz \
    "https://github.com/canonical/dqlite/archive/v${DQLITE_VERSION}.tar.gz"

echo "--- BUILD LIBUV ---"

dir="libuv-${UV_VERSION}-$suffix"
export UV_CFLAGS="-I${deps_dir}/$dir/include"
export UV_LIBS="-L${deps_dir}/$dir/.libs/ -luv"
export UV_LIB="${deps_dir}/$dir/.libs/libuv.a"
if [ ! -f ${UV_LIB} ]; then
    mkdir -p $dir
    tar --strip-components=1 -C $dir -xf libuv-${UV_VERSION}.tar.gz
    cd $dir

    sh autogen.sh
    ./configure
    make -j8
fi

cd "${deps_dir}"
echo "--- BUILD RAFT ---"

dir="raft-${RAFT_VERSION}-$suffix"
export RAFT_CFLAGS="-I${deps_dir}/$dir/include"
export RAFT_LIBS="-L${deps_dir}/$dir/.libs -lraft"
export RAFT_LIB="${deps_dir}/$dir/.libs/libraft.a"
if [ ! -f ${RAFT_LIB} ]; then
    mkdir -p $dir
    tar --strip-components=1 -C $dir -xf raft-${RAFT_VERSION}.tar.gz
    cd $dir
    patch -p1 < ../raft.patch

    autoreconf -i
    ./configure
    make -j8
fi

cd "${deps_dir}"
echo "--- BUILD SQLITE ---"

dir="sqlite-${SQLITE_VERSION}-$suffix"
export SQLITE_CFLAGS="-I${deps_dir}/$dir"
export SQLITE_LIBS="-L${deps_dir}/$dir/.libs -lsqlite3"
export SQLITE_LIB="${deps_dir}/$dir/.libs/libsqlite3.a"
if [ ! -f ${SQLITE_LIB} ]; then
    mkdir -p $dir
    tar --strip-components=1 -C $dir -xf sqlite-${SQLITE_VERSION}.tar.gz
    cd $dir

    ./configure
    make -j8
fi

cd "${deps_dir}"
echo "--- BUILD DQLITE ---"

dir="dqlite-${DQLITE_VERSION}-$suffix"
export DQLITE_CFLAGS="-I${deps_dir}/$dir/include"
export DQLITE_LIBS="-L${deps_dir}/$dir/.libs -ldqlite"
export DQLITE_LIB="${deps_dir}/$dir/.libs/libdqlite.a"
if [ ! -f ${DQLITE_LIB} ]; then
    mkdir -p $dir
    tar --strip-components=1 -C $dir -xf dqlite-${DQLITE_VERSION}.tar.gz
    cd $dir
    patch -p1 < ../dqlite.patch

    autoreconf -i
    ./configure
    make -j8
fi
