#!/bin/sh -e
# Copyright 2021 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

#
# This example script allows to see the existing Label and create a new version of it
# Please check the images URLs in Label definitions below
#

token=$1
[ "$token" ] || exit 1

label=macos1015-xcode122_vmx

# It's a bit dirty, but works for now - probably better to create API call to find the latest label
curr_label=$(curl -s -u "admin:$token" -k 'https://127.0.0.1:8001/api/v1/label/?filter=name="'$label'"' | sed 's/},{/},\n{/g' | tail -1)
curr_version="$(echo "$curr_label" | grep -o '"version": *[0-9]\+' | tr -dc '0-9')"
echo "Current label '$label:$curr_version': $curr_label"

[ "x$curr_version" != "x" ] || curr_version=0
new_version=$(($curr_version+1))

echo
echo "Create the new version of Label '$label:$new_version' ?"
echo "Press any key to create or Ctrl-C to abort"
read w1

label_id=$(curl -s -u "admin:$token" -k -X POST -H 'Content-Type: application/json' -d '{"name":"'$label'", "version":'$new_version', "driver":"vmx",
    "definition": {
        "image": "macos1015-xcode122-ci",
        "images": {
            "macos1015":             "https://artifact-storage/aquarium/image/vmx/macos1015-VERSION/macos1015-VERSION.tar.xz",
            "macos1015-xcode122":    "https://artifact-storage/aquarium/image/vmx/macos1015-xcode122-VERSION/macos1015-xcode122-VERSION.tar.xz",
            "macos1015-xcode122-ci": "https://artifact-storage/aquarium/image/vmx/macos1015-xcode122-ci-VERSION/macos1015-xcode122-ci-VERSION.tar.xz"
        },
        "requirements": {
            "cpu": 14,
            "ram": 12,
            "disks": {
                "xcode122": {
                    "type": "hfs+",
                    "size": 100,
                    "reuse": true
                }
            }
        }
    },
    "metadata": {
        "JENKINS_AGENT_WORKSPACE": "/Volumes/xcode122"
    }
}' https://127.0.0.1:8001/api/v1/label/ | grep -o '"UID": *"[^"]\+"' | cut -d':' -f 2 | tr -d ' "')

echo "Created Label ID: ${label_id}"
