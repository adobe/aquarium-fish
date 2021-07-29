#/bin/sh
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
echo '---------------------- YAML Lint ----------------------'
echo
# cytopia/yamllint:1.22
# WARN: Use only image ID, not tags
# Login:
#   $ docker login docker-hub-remote.dr-uw2.adobeitc.com
#   username: <login>
#   password: <API key from https://artifactory-uw2.adobeitc.com/artifactory/webapp/#/profile>
docker run --rm -v "${root_dir}:/data" docker-hub-remote.dr-uw2.adobeitc.com/cytopia/yamllint@sha256:ea346562a5e8ec0ad7a90f197d21fe1873520e44066b1367c0eedfe5991f3a1e --strict cmd docs lib
errors=$((${errors}+$?))

exit ${errors}
