#/bin/sh
# Copyright 2021 Adobe. All rights reserved.
# This file is licensed to you under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License. You may obtain a copy
# of the License at http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software distributed under
# the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
# OF ANY KIND, either express or implied. See the License for the specific language
# governing permissions and limitations under the License.

# Script to simplify the style check process

root_dir=$(realpath "$(dirname "$0")")
errors=0

echo
echo '---------------------- Custom Checks ----------------------'
echo
for f in `git ls-files`; do
    # Check text files
    if file "$f" | grep -q 'text$'; then
        # Ends with newline as POSIX requires
        if [ -n "$(tail -c 1 "$f")" ]; then
            echo "Not ends with newline: $f"
            errors=$((${errors}+1))
        fi
    fi
done


echo
echo '---------------------- GoFmt verify ----------------------'
echo
reformat=$(gofmt -l .)
if [ "${reformat}" ]; then
    echo "Please run 'gofmt -w .': \n${reformat}"
    errors=$((${errors}+$(echo "${reformat}" | wc -l)))
fi


echo
echo '---------------------- GoModTidy verify ----------------------'
echo
cp -af go.mod go.sum /tmp/
tidy=$(go mod tidy -v)
if [ "${tidy}" -o "x$(date -r /tmp/go.mod ; date -r /tmp/go.sum)" != "x$(date -r go.mod ; date -r go.sum)" ]; then
    echo "Please run 'go mod tidy -v' \n${tidy}"
    errors=$((${errors}+$(echo "${tidy}" | wc -l)))
fi
mv /tmp/go.mod /tmp/go.sum ./


echo
echo '---------------------- GoVet verify ----------------------'
echo
vet=$(go vet ./... 2>&1)
if [ "${vet}" ]; then
    echo "Please fix the issues: \n${vet}"
    errors=$(((${errors}+$(echo "${vet}" | wc -l))/2))
fi


echo
echo '---------------------- YAML Lint ----------------------'
echo
docker run --rm -v "${root_dir}:/data" cytopia/yamllint:1.22 --strict cmd docs lib
errors=$((${errors}+$?))

exit ${errors}
