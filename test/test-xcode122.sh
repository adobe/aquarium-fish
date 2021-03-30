#!/bin/sh -e

token=$1

curl -u "admin:$token" -v -X POST -d '{"name":"xcode12.2", "version":1, "driver":"vmx",
"definition": {
    "image": "macos-1015-ci-xcode122",
    "images": {
        "macos-1015": "https://artifact-storage/aquarium/image/macos-1015-VERSION/macos-1015-VERSION.tar.xz",
        "macos-1015-ci": "https://artifact-storage/aquarium/image/macos-1015-ci-VERSION/macos-1015-ci-VERSION.tar.xz",
        "macos-1015-ci-xcode122": "https://artifact-storage/aquarium/image/macos-1015-ci-xcode122-VERSION/macos-1015-ci-xcode122-VERSION.tar.xz"
    },
    "requirements": {
        "cpu": 14,
        "ram": 12,
        "disks": {
            "xcode122": {
                "size": 100,
                "reuse": true
            }
        }
    }
},
"metadata": {
    "JENKINS_AGENT_WORKSPACE": "/Volumes/xcode122"
}}' 127.0.0.1:8001/api/v1/label/

echo "wait before get label from another node"
read w1

curl -u "admin:$token" -v -X GET 127.0.0.1:8001/api/v1/label/1

echo "wait before run application"
read w1

curl -u "admin:$token" -v -X POST -d '{"label_id":1, "metadata":{
    "JENKINS_URL": "http://172.16.1.1:8085/",
    "JENKINS_AGENT_SECRET": "03839eabcf945b1e780be8f9488d264c4c57bf388546da9a84588345555f29b0",
    "JENKINS_AGENT_NAME": "test-node"
}}' 127.0.0.1:8001/api/v1/application/

echo "wait before check the application resource"
read w1

curl -u "admin:$token" -v -X GET 127.0.0.1:8001/api/v1/application/1/resource

echo "wait before deallocate the application resource"
read w1

curl -u "admin:$token" -v -X GET 127.0.0.1:8001/api/v1/application/1/deallocate
