#!/usr/bin/env bash

set -euxo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
INFRA_DIR="${SCRIPT_DIR}"

ECS_LOG_GROUP_NAME="$(cd "${INFRA_DIR}" && terraform output -json ecs_log_group | jq -r)"
AWS_REGION="$(cd "${INFRA_DIR}" && terraform output -json aws_region | jq -r)"

mkdir -p /tmp/mount
cwl-mount --region "${AWS_REGION}" mount /tmp/mount --log-group-name "${ECS_LOG_GROUP_NAME}" 
