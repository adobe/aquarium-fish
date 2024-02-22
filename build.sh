#!/bin/sh -e
# Copyright 2021 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

MAXJOBS=$1
[ "x$MAXJOBS" != 'x' ] || MAXJOBS=4

root_dir=$(cd "$(dirname "$0")"; echo "$PWD")
echo "ROOT DIR: ${root_dir}"
cd "${root_dir}"

# Disabling cgo in order to not link with libc and utilize static linkage binaries
# which will help to not relay on glibc on linux and be truely independend from OS
export CGO_ENABLED=0

echo "--- GENERATE CODE FOR AQUARIUM-FISH ---"
# Install oapi-codegen if it's not available or version is not the same with go.mod
gopath=$(go env GOPATH)
req_ver=$(grep -F 'github.com/deepmap/oapi-codegen' go.mod | cut -d' ' -f 2)
curr_ver="$(PATH="$gopath/bin:$PATH" oapi-codegen --version 2>/dev/null | tail -1 || true)"
if [ "$curr_ver" != "$req_ver" ]; then
    go install "github.com/deepmap/oapi-codegen/cmd/oapi-codegen@$req_ver"
fi
# Cleanup the old generated files
find ./lib -name '*.gen.go' -delete

# Run code generation
PATH="$gopath/bin:$PATH" go generate -v ./lib/...
# Making LabelDefinitions an actual type to attach GORM-needed Scanner/Valuer functions to it to
# make the array a json document and store in the DB row as one item
# TODO: https://github.com/deepmap/oapi-codegen/issues/859
sed -i.bak 's/^type LabelDefinitions = /type LabelDefinitions /' lib/openapi/types/types.gen.go
rm -f lib/openapi/types/types.gen.go.bak

# Prepare version number as overrides during link
mod_name=$(grep '^module' "${root_dir}/go.mod" | cut -d' ' -f 2)
git_version="$(git describe --tags --match 'v*')$([ "$(git diff)" = '' ] || echo '-dirty')"
version_flags="-X '$mod_name/lib/build.Version=${git_version}' -X '$mod_name/lib/build.Time=$(date -u +%y%m%d.%H%M%S)'"
BINARY_NAME="aquarium-fish-$git_version"

# Doing check after generation because generated sources requires additional modules
./check.sh

echo
echo "--- RUN UNIT TESTS ---"
go test -ldflags="$version_flags" -v ./lib/...

echo
echo "--- BUILD ${BINARY_NAME} ($MAXJOBS in parallel) ---"

if [ "x${RELEASE}" != "x" ]; then
    export GIN_MODE=release
    os_list='linux darwin windows freebsd openbsd'
    arch_list='amd64 arm64'
else
    echo "--- WARNING: build DEBUG mode ---"
    os_list="$(go env GOOS)"
    arch_list="$(go env GOARCH)"
fi


# Run parallel builds but no more than limit (gox doesn't support all the os/archs we need)
pwait() {
    # Note: Dash really don't like jobs to be executed in a pipe or in other shell, soooo...
    while jobs > /tmp/jobs_list.tmp; do
        [ $(cat /tmp/jobs_list.tmp | wc -l) -ge $1 ] || break
        sleep 1
    done
}

# If use it directly - it causes "go tool dist: signal: broken pipe"
go tool dist list > /tmp/go_tool_dist_list.txt

for GOOS in $os_list; do
    for GOARCH in $arch_list; do
        name="$BINARY_NAME.${GOOS}_${GOARCH}"

        if ! grep -q "^${GOOS}/${GOARCH}$" /tmp/go_tool_dist_list.txt; then
            echo "Skipping: $name as not supported by go"
            continue
        fi

        echo "Building: $name ..."
        rm -f "$name" "$name.log" "$name.zip" "$name.tar.xz"
        GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="-s -w $version_flags" -o "$name" ./cmd/fish > "$name.log" 2>&1 &
        pwait $MAXJOBS
    done
done

wait

# Check build logs for errors
errorcount=0
for GOOS in $os_list; do
    for GOARCH in $arch_list; do
        name="$BINARY_NAME.${GOOS}_${GOARCH}"
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
