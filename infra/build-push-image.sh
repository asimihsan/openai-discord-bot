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
SRC_DIR="${SCRIPT_DIR}/../src"
APP_NAME="openai-discord-bot"
TAG="${1:-latest}"

ECR_REPOSITORY_URL="$(cd "${INFRA_DIR}" && terraform output -json ecr_repository_url | jq -r)"
AWS_REGION="$(cd "${INFRA_DIR}" && terraform output -json aws_region | jq -r)"
ECR_DESTINATION="${ECR_REPOSITORY_URL}:${TAG}"

aws ecr get-login-password --region "${AWS_REGION}" | docker login --username AWS --password-stdin "${ECR_REPOSITORY_URL}"
(cd "${SRC_DIR}" && docker buildx build --platform linux/arm64 -t "${ECR_DESTINATION}" .)
docker push "${ECR_DESTINATION}"
