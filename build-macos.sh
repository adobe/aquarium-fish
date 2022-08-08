#!/bin/bash -e
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
[ "x$suffix" != "x" ] || suffix="$(go env GOOS)_$(go env GOARCH)"

root_dir=$(dirname "`realpath "$0"`")

cd "${root_dir}"

export GOOS=darwin

source _build.sh

echo "--- SIGN AQUARIUM-FISH ---"
# Sign the binary to remove the sandbox restrictions
codesign -s - -f --entitlements macos.plist "aquarium-fish.$suffix"
