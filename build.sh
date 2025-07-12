#!/bin/sh -e
# Copyright 2021-2025 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Author: Sergei Parshev (@sparshev)

MAXJOBS=$1
[ "x$MAXJOBS" != 'x' ] || MAXJOBS=4

root_dir=$(cd "$(dirname "$0")"; echo "$PWD")
echo "ROOT DIR: ${root_dir}"
cd "${root_dir}"

# Disabling cgo in order to not link with libc and utilize static linkage binaries
# which will help to not relay on glibc on linux and be truely independend from OS
export CGO_ENABLED=0

echo "--- GENERATE CODE FOR AQUARIUM-FISH ---"

# Install required tools if not available
gopath=$(go env GOPATH)
export PATH="$PATH:$gopath/bin"

# Install buf for protobuf management if not available
if ! command -v buf >/dev/null 2>&1; then
    go install github.com/bufbuild/buf/cmd/buf@v1.54.0
fi

if ! command -v protoc-gen-go >/dev/null 2>&1; then
    # Version is from go.mod
    go install google.golang.org/protobuf/cmd/protoc-gen-go
fi

if ! command -v protoc-gen-connect-go >/dev/null 2>&1; then
    # Version is from go.mod
    go install connectrpc.com/connect/cmd/protoc-gen-connect-go@v1.18.1
fi

# Run code generation
go generate -v .

echo
echo "--- BUILD WEB DASHBOARD ---"
# Build the web dashboard
if [ -d "web" ]; then
    echo "Building web dashboard..."
    cd web
    ./build.sh
    cd ..

    rm -rf lib/web/dist
    cp -a web/build/client lib/web/dist
    echo "Web dashboard build completed"
else
    echo "Web directory not found, skipping web build"
fi

# If ONLYGEN is specified - skip the build
[ -z "$ONLYGEN" ] || exit 0

# Doing check after generation because generated sources requires additional modules
[ "$SKIPCHECK" ] || ./check.sh

echo
echo "--- RUN UNIT TESTS ---"
# Unit tests should not consume more then 5 sec per run - for that we have integration tests
go test -v -failfast -count=1 -timeout=5s -v ./lib/...

echo
echo "--- BUILD ${BINARY_NAME} ($MAXJOBS in parallel) ---"

if [ "x${RELEASE}" != "x" ]; then
    echo "--- RELEASE ---"
    export GIN_MODE=release
    os_list='linux darwin windows freebsd openbsd'
    arch_list='amd64 arm64'
    build_command="go build"
    # Removing debug symbols and omitting DWARF symbol table to reduce binary size
    ld_opts="-s -w"
else
    echo "--- DEBUG ---"
    debug_suffix="-debug"
    os_list="$(go env GOOS)"
    arch_list="$(go env GOARCH)"
    # Building with race detectors to capture them during integration testing
    build_command="go build -race -tags debug"
    # Unsetting cgo to allow -race to work propely
    unset CGO_ENABLED
fi

# Prepare version number as overrides during link
mod_name=$(grep '^module' "${root_dir}/go.mod" | cut -d' ' -f 2)
git_version="$(git describe --tags --match 'v*')$([ "$(git diff HEAD)" = '' ] || echo '-dirty')"
version_flags="-X '$mod_name/lib/build.Version=${git_version}${debug_suffix}' -X '$mod_name/lib/build.Time=$(date -u +%y%m%d.%H%M%S)'"
BINARY_NAME="aquarium-fish-$git_version"

# Run parallel builds but no more than limit (gox doesn't support all the os/archs we need)
pwait() {
    # Note: Dash really don't like jobs to be executed in a pipe or in other shell, soooo...
    # Using "jobs -p" to show only PIDs (because it could be multiline)
    # Unfortunately "jobs -r" is not supported in dash, not a big problem with sleep for 1 sec
    while jobs -p > /tmp/jobs_list.tmp; do
        # Cleanup jobs list, otherwise "jobs -p" will stay the same forever
        jobs > /dev/null 2>&1
        [ $(cat /tmp/jobs_list.tmp | wc -l) -ge "$MAXJOBS" ] || break
        sleep 1
    done
}

# If use it directly - it causes "go tool dist: signal: broken pipe"
go tool dist list > /tmp/go_tool_dist_list.txt

for GOOS in $os_list; do
    for GOARCH in $arch_list; do
        name="$BINARY_NAME${debug_suffix}.${GOOS}_${GOARCH}"

        if ! grep -q "^${GOOS}/${GOARCH}$" /tmp/go_tool_dist_list.txt; then
            echo "Skipping: $name as not supported by go"
            continue
        fi

        echo "Building: $name ..."
        if [ "x${RELEASE}" = "x" ]; then
            echo "WARNING: It's DEBUG binary with instrumentation"
        fi
        rm -f "$name" "$name.log" "$name.zip" "$name.tar.xz"
        GOOS=$GOOS GOARCH=$GOARCH $build_command -ldflags="$ld_opts $version_flags" -o "$name" . > "$name.log" 2>&1 &
        pwait $MAXJOBS
    done
done

wait

# Check build logs for errors
errorcount=0
for GOOS in $os_list; do
    for GOARCH in $arch_list; do
        name="$BINARY_NAME${debug_suffix}.${GOOS}_${GOARCH}"
        # Log file is not here - build was skipped
        [ -f "$name.log" ] || continue
        # Binary is not here - build error happened
        if [ ! -f "$name" ]; then
            echo
            echo "--- ERROR: $name ---"
            cat "$name.log"
            errorcount=$(($errorcount+1))
        elif [ -s "$name.log" ]; then
            echo
            echo "--- WARNING: $name ---"
            cat "$name.log"
        fi
        rm -f "$name.log"
    done
done

[ $errorcount -eq 0 ] || exit $errorcount

if [ "x${RELEASE}" != "x" ]; then
    echo
    echo "--- ARCHIVE ${BINARY_NAME} ($MAXJOBS in parallel) ---"

    # Pack the artifact archives
    for GOOS in $os_list; do
        for GOARCH in $arch_list; do
            name="$BINARY_NAME.${GOOS}_${GOARCH}"
            [ -f "$name" ] || continue

            echo "Archiving: $(du -h "$name") ..."
            mkdir "$name.dir"
            bin_name='aquarium-fish'
            [ "$GOOS" != "windows" ] || bin_name="$bin_name.exe"

            cp -a "$name" "$name.dir/$bin_name"
            $(
                cd "$name.dir"
                tar -cJf "../$name.tar.xz" "$bin_name" >/dev/null 2>&1
                zip "../$name.zip" "$bin_name" >/dev/null 2>&1
                cd .. && rm -rf "$name.dir"
            ) &
            pwait $MAXJOBS
        done
    done

    wait
fi
