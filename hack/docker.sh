#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# This script holds docker related functions.

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
REGISTRY=${REGISTRY:-"ccr.ccs.tencentyun.com/qcloud-ti-platform"}
VERSION=${VERSION:="unknown"}

function build_images() {
  local target="$1"
  docker build -t ${REGISTRY}/${target}:${VERSION} -f ${REPO_ROOT}/cluster/images/${target}/Dockerfile ${REPO_ROOT}
}

build_images $@
