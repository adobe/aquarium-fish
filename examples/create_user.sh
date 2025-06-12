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

#
# This example script creates test user with predefined password
#

token=$1
[ "$token" ] || exit 1
hostport=$2
[ "$hostport" ] || hostport=localhost:8001

# Create user
curl -s -u "admin:$token" -k -X POST -H 'Content-Type: application/json' -d '{
    "name":"test-user",
    "password":"test-user-password"
}' "https://$hostport/api/v1/user/"

# Assign roles
curl -s -u "admin:$token" -k -X POST -H 'Content-Type: application/json' -d '[
    "Power", "User"
]' "https://$hostport/api/v1/user/test-user/roles"
