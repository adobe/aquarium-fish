#!/bin/sh -e
# Copyright 2021 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Build fish for macos (directly on macos host)

[ "x$suffix" != "x" ] || suffix="$1"
[ "x$suffix" != "x" ] || suffix="$(uname -s)_$(uname -m)"

root_dir=$(dirname "`realpath "$0"`")

deps_dir=${root_dir}/deps
. deps/build-deps.sh

cd "${root_dir}"

# MacOS doesn't support static executables, so using .a to compile-in the dependencies at least
export SET_CGO_LDFLAGS="${CGO_LDFLAGS} ${DQLITE_LIB} ${SQLITE_LIB} ${RAFT_LIB} ${UV_LIB}"

source _build.sh

echo "--- SIGN AQUARIUM-FISH ---"
# Sign the binary to remove the sandbox restrictions
codesign -s - -f --entitlements macos.plist "aquarium-fish.$suffix"
