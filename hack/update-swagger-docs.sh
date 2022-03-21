#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
source "${REPO_ROOT}"/hack/util.sh

util::verify_go_version

go run hack/tools/swagger/generateswagger.go > api/openapi-spec/swagger.json

# Delete trash of generating swagger doc
rm -rf /tmp/karmada-swagger
