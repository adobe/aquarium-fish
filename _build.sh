#!/bin/sh -e
# Copyright 2021 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Common build script - do not run directly
# Use ./build-linux.sh / ./build-macos.sh instead

[ "x$suffix" != "x" ] || suffix="$1"
[ "x$suffix" != "x" ] || suffix="$(go env GOOS)_$(go env GOARCH)"

echo "ROOT DIR: ${root_dir}"
cd "${root_dir}"


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
sed -i 's/^type LabelDefinitions = /type LabelDefinitions /' lib/openapi/types/types.gen.go

# Doing check after generation because generated sources requires additional modules
./check.sh

if [ "x${RELEASE}" != "x" ]; then
    export GIN_MODE=release
else
    echo
    echo "--- WARNING: build DEBUG mode ---"
fi

# Prepare version number as overrides during link
mod_name=$(grep '^module' "${root_dir}/go.mod" | cut -d' ' -f 2)
git_version="$(git describe --tags --match 'v*')$([ "$(git diff)" = '' ] || echo '-dirty')"
version_flags="-X '$mod_name/lib/build.Version=${git_version}' -X '$mod_name/lib/build.Time=$(date -u +%y%m%d.%H%M%S)'"


echo
echo "--- RUN UNIT TESTS ---"
go test -ldflags="$version_flags" -v ./lib/...

echo
echo "--- BUILD AQUARIUM-FISH ${git_version} ---"
go build -ldflags="-s -w $version_flags" -a -o "aquarium-fish.$suffix" ./cmd/fish

# Remove debug symbols
strip "aquarium-fish.$suffix"
