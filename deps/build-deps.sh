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

srcdir="${deps_dir}/libuv-${UV_VERSION}"
builddir="$srcdir-$suffix"
export UV_CFLAGS="-I$builddir/include"
export UV_LIBS="-L$builddir/.libs/ -luv"
export UV_LIB="$builddir/.libs/libuv.a"
if [ ! -d "$srcdir" ]; then
    mkdir -p "$srcdir"
    tar --strip-components=1 -C "$srcdir" -xf libuv-${UV_VERSION}.tar.gz
fi
mkdir -p "$builddir"
cp -au "$srcdir"/* "$builddir"
cd $builddir
if [ ! -f ${UV_LIB} ]; then
    sh autogen.sh
    ./configure
fi
make -j8

cd "${deps_dir}"
echo "--- BUILD RAFT ---"

srcdir="${deps_dir}/raft-${RAFT_VERSION}"
builddir="$srcdir-$suffix"
export RAFT_CFLAGS="-I$builddir/include"
export RAFT_LIBS="-L$builddir/.libs -lraft"
export RAFT_LIB="$builddir/.libs/libraft.a"
if [ ! -d "$srcdir" ]; then
    mkdir -p "$srcdir"
    tar --strip-components=1 -C "$srcdir" -xf raft-${RAFT_VERSION}.tar.gz
    patch -p1 -d "$srcdir" < "${deps_dir}/raft.patch"
fi
mkdir -p "$builddir"
cp -au "$srcdir"/* "$builddir"
cd $builddir
if [ ! -f ${RAFT_LIB} ]; then
    autoreconf -i
    ./configure
fi
make -j8

cd "${deps_dir}"
echo "--- BUILD SQLITE ---"

srcdir="${deps_dir}/sqlite-${SQLITE_VERSION}"
builddir="$srcdir-$suffix"
export SQLITE_CFLAGS="-I$builddir"
export SQLITE_LIBS="-L$builddir/.libs -lsqlite3"
export SQLITE_LIB="$builddir/.libs/libsqlite3.a"
if [ ! -d "$srcdir" ]; then
    mkdir -p "$srcdir"
    tar --strip-components=1 -C "$srcdir" -xf sqlite-${SQLITE_VERSION}.tar.gz
fi
mkdir -p "$builddir"
cp -au "$srcdir"/* "$builddir"
cd $builddir
if [ ! -f ${SQLITE_LIB} ]; then
    ./configure
fi
make -j8

cd "${deps_dir}"
echo "--- BUILD DQLITE ---"

srcdir="${deps_dir}/dqlite-${DQLITE_VERSION}"
builddir="$srcdir-$suffix"
export DQLITE_CFLAGS="-I$builddir/include"
export DQLITE_LIBS="-L$builddir/.libs -ldqlite"
export DQLITE_LIB="$builddir/.libs/libdqlite.a"
if [ ! -d "$srcdir" ]; then
    mkdir -p "$srcdir"
    tar --strip-components=1 -C "$srcdir" -xf dqlite-${DQLITE_VERSION}.tar.gz
    patch -p1 -d "$srcdir" < "${deps_dir}/dqlite.patch"
fi
mkdir -p "$builddir"
cp -au "$srcdir"/* "$builddir"
cd $builddir
if [ ! -f ${DQLITE_LIB} ]; then
    autoreconf -i
    ./configure
fi
make -j8
