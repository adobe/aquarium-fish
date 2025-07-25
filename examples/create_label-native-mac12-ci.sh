#!/bin/sh -e
# Copyright 2023-2025 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Author: Sergei Parshev (@sparshev)

#
# This example script allows to see the existing Label and create a new version of it
# Please check the images URLs in Label definitions below
#

token=$1
[ "$token" ] || exit 1
hostport=$2
[ "$hostport" ] || hostport=localhost:8001

label=mac12_native

curr_label=$(curl -s -u "admin:$token" -X POST --header "Content-Type: application/json" \
    -d "{\"name\":\"$label\",\"version\":\"last\"}" -k "https://$hostport/grpc/aquarium.v2.LabelService/List" | sed 's/^.*"data":\[//' | sed 's/\]}$//')
curr_version="$(echo "$curr_label" | grep -o '"version": *[0-9]\+' | tr -dc '0-9')"
echo "Current label '$label:$curr_version': $curr_label"

[ "x$curr_version" != "x" ] || curr_version=0
new_version=$(($curr_version+1))

echo
echo "Create the new version of Label '$label:$new_version' ?"
echo "Press any key to create or Ctrl-C to abort"
read w1

label_id=$(curl -s -u "admin:$token" -k -X POST -H 'Content-Type: application/yaml' -d '---
label:
  name: "'$label'"
  version: '$new_version'
  definitions:
    - driver: native
      options:
        images:  # For test purposes images are used as symlink to aquarium-bait/out so does not need checksum
          - url: https://artifact-storage/aquarium/image/native/mac/mac-VERSION.tar.xz
            tag: ws
          - url: https://artifact-storage/aquarium/image/native/mac-ci/mac-ci-VERSION.tar.xz
            tag: ws
        groups:
          - staff
        entry: "{{ .Disks.ws }}/init.sh"
      resources:
        node_filter:
          - OS:darwin
          - OSVersion:12.*
          - Arch:x86_64
        cpu: 4
        ram: 8
        disks:
          ws:
            size: 10
        network: Name:test-vpc
  metadata:
    JENKINS_AGENT_WORKSPACE: "{{ .Disks.ws }}"
' "https://$hostport/grpc/aquarium.v2.LabelService/Create" | grep -o '"uid": *"[^"]\+"' | cut -d':' -f 2 | tr -d ' "')

echo "Created Label ID: ${label_id}"
