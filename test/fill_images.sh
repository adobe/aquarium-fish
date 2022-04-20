#!/bin/sh
# Copyright 2021 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

#
# Script updates the specified fish_vmx_images directory based on the out dir in aquarium-bait repo
#
# Usage:
#  $ ./test/fill_images.sh <relative/path/to/workdir/fish_vmx_images> </abs/path/to/aquarium-bait/out>
#

IMAGES_DIR=$1
OUT_DIR=$2

[ -d "$IMAGES_DIR" ] || exit 1
[ -d "$OUT_DIR" ] || exit 2

if [ "x$(dirname "$OUT_DIR" | cut -c1)" != 'x/' ]; then
    echo "ERROR: Need absolute path to the out directory: $OUT_DIR"
    exit 3
fi

for img in "$OUT_DIR"/*; do
    # Processing only directories
    [ -d "$img" ] || continue

    echo "Processing '$img'..."

    # Getting the image name
    name=$(basename "$img" | rev | cut -d- -f2- | rev)
    echo "  name: $name"

    rm -rf "$IMAGES_DIR/$name-VERSION"
    mkdir -p "$IMAGES_DIR/$name-VERSION"
    ln -s "$img" "$IMAGES_DIR/$name-VERSION/"

    echo "  done: $(ls -l "$IMAGES_DIR/$name-VERSION/" | tail -1)"
done
