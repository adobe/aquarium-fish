#!/bin/sh -e
# Build fish for macos (directly on macos host)

[ "x$suffix" != "x" ] || suffix="$1"
[ "x$suffix" != "x" ] || suffix="$(uname -s)_$(uname -m)"

root_dir=$(dirname "`realpath "$0"`")

deps_dir=${root_dir}/deps
. deps/build-deps.sh

cd "${root_dir}"

# MacOS doesn't support static executables, so using .a to compile-in the dependencies at least
export CGO_LDFLAGS="${CGO_LDFLAGS} ${DQLITE_LIB} ${SQLITE_LIB} ${RAFT_LIB} ${UV_LIB}"

source _build.sh

echo "--- SIGN AQUARIUM-FISH ---"
# Sign the binary to remove the sandbox restrictions
codesign -s - -f --entitlements macos.plist "aquarium-fish.$suffix"
