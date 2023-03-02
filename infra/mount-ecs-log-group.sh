#!/usr/bin/env bash

#
# Copyright (C) 2023 Asim Ihsan
# SPDX-License-Identifier: AGPL-3.0-only
#
# This program is free software: you can redistribute it and/or modify it under
# the terms of the GNU Affero General Public License as published by the Free
# Software Foundation, version 3.
#
# This program is distributed in the hope that it will be useful, but WITHOUT ANY
# WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
# PARTICULAR PURPOSE. See the GNU Affero General Public License for more details.
#
# You should have received a copy of the GNU Affero General Public License along
# with this program. If not, see <https://www.gnu.org/licenses/>
#

set -euxo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
INFRA_DIR="${SCRIPT_DIR}"

ECS_LOG_GROUP_NAME="$(cd "${INFRA_DIR}" && terraform output -json ecs_log_group | jq -r)"
AWS_REGION="$(cd "${INFRA_DIR}" && terraform output -json aws_region | jq -r)"

mkdir -p /tmp/mount
cwl-mount --region "${AWS_REGION}" mount /tmp/mount --log-group-name "${ECS_LOG_GROUP_NAME}" 
