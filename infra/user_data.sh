#!/usr/bin/env bash

set -euo pipefail

echo ECS_CLUSTER=${cluster_name} >> /etc/ecs/ecs.config
echo ECS_CONTAINER_INSTANCE_PROPAGATE_TAGS_FROM=ec2_instance >> /etc/ecs/ecs.config
echo ECS_ENABLE_SPOT_INSTANCE_DRAINING=true >> /etc/ecs/ecs.config
