#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
INFRA_DIR="${SCRIPT_DIR}"
SRC_DIR="${SCRIPT_DIR}/../src"
APP_NAME="openai-discord-bot"

ECR_REPOSITORY_URL="$(cd "${INFRA_DIR}" && terraform output -json ecr_repository_url | jq -r)"
AWS_REGION="$(cd "${INFRA_DIR}" && terraform output -json aws_region | jq -r)"

aws ecr get-login-password --region "${AWS_REGION}" | docker login --username AWS --password-stdin "${ECR_REPOSITORY_URL}"
(cd "${SRC_DIR}" && docker buildx build --platform linux/arm64 -t "${ECR_REPOSITORY_URL}:${APP_NAME}" .)
docker push "${ECR_REPOSITORY_URL}:${APP_NAME}"
