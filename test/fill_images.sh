#!/bin/sh
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
