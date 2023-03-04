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

resource "aws_ecr_repository" "ecr_repo" {
  name                 = "${var.application}-${random_id.application.hex}-${local.environment}"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  tags = merge(local.tags, {
  })
}

data "aws_ecr_image" "ecr_image" {
  repository_name = aws_ecr_repository.ecr_repo.name
  image_tag       = "latest"
}

resource "aws_cloudwatch_log_group" "log_group" {
  name              = "/aws/ecs/${var.application}-${local.environment}-${random_id.application.hex}"
  retention_in_days = 1

  tags = merge(local.tags, {
  })
}

resource "aws_ecs_cluster" "ecs_cluster" {
  name = "${var.application}-${local.environment}-${random_id.application.hex}"

  configuration {
    execute_command_configuration {
      logging = "OVERRIDE"
      log_configuration {
        cloud_watch_encryption_enabled = true
        cloud_watch_log_group_name     = aws_cloudwatch_log_group.log_group.name
      }
    }
  }

  tags = merge(local.tags, {
  })
}

# IAM task role for ECS tasks.
resource "aws_iam_role" "ecs_task_role" {
  name               = "${var.application}-${local.environment}-${random_id.application.hex}-ecs-task-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
      },
    ]
  })

  tags = merge(local.tags, {
  })
}

# Attach SSM policies to ECS task role.
resource "aws_iam_role_policy_attachment" "ecs_task_role_ssm" {
  role       = aws_iam_role.ecs_task_role.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

# Attach CloudWatch policies to ECS task role.
resource "aws_iam_role_policy_attachment" "ecs_task_role_cloudwatch" {
  role       = aws_iam_role.ecs_task_role.name
  policy_arn = "arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy"
}

resource "aws_iam_policy" "ecs_task_role_dynamodb" {
  name        = "${var.application}-${local.environment}-${random_id.application.hex}-ecs-task-role-dynamodb"
  description = "IAM policy for ECS task role that grants GetItem, PutItem, DeleteItem on ${aws_dynamodb_table.bot_lock_table.name} table."
  policy      = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:DeleteItem",
        ]
        Effect   = "Allow"
        Resource = [
          "${aws_dynamodb_table.bot_lock_table.arn}",
        ]
      },
    ]
  })

  tags = merge(local.tags, {
  })
}

resource "aws_iam_role_policy_attachment" "ecs_task_role_dynamodb" {
  role       = aws_iam_role.ecs_task_role.name
  policy_arn = aws_iam_policy.ecs_task_role_dynamodb.arn
}

resource "aws_iam_policy" "ecs_task_role_secretsmanager" {
  name        = "${var.application}-${local.environment}-${random_id.application.hex}-ecs-task-role-secretsmanager"
  description = "IAM policy for ECS task role that grants secretsmanager:GetSecretValue for each secret in secret_names_for_ecs_task."
  policy      = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = [
          "secretsmanager:GetSecretValue",
        ]
        Effect   = "Allow"
        Resource = [
          "${aws_secretsmanager_secret.discord_application_id.arn}",
          "${aws_secretsmanager_secret.discord_public_key.arn}",
          "${aws_secretsmanager_secret.discord_token.arn}",
          "${aws_secretsmanager_secret.discord_guild_id.arn}",
          "${aws_secretsmanager_secret.openai_token.arn}",
        ]
      },
    ]
  })

  tags = merge(local.tags, {
  })
}

resource "aws_iam_role" "ecs_task_execution_role" {
  name               = "${random_id.application.hex}-ecs-task-execution-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
      },
    ]
  })

  tags = merge(local.tags, {
  })
}

resource "aws_iam_role_policy_attachment" "ecs_task_execution_role_secretsmanager" {
  role       = aws_iam_role.ecs_task_execution_role.name
  policy_arn = aws_iam_policy.ecs_task_role_secretsmanager.arn
}

resource "aws_iam_role_policy_attachment" "ecs_task_execution_role_ecr" {
  role       = aws_iam_role.ecs_task_execution_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# Deploy ECS image var.application to ECS cluster.
# See: https://www.sufle.io/blog/keeping-secrets-as-secret-on-amazon-ecs-using-terraform
resource "aws_ecs_task_definition" "task_definition" {
  family                   = "${var.application}-${local.environment}-${random_id.application.hex}"
  execution_role_arn       = aws_iam_role.ecs_task_execution_role.arn
  requires_compatibilities = ["EC2"]
  cpu                      = "768"
  memory                   = "128"
  network_mode             = "host"
  task_role_arn            = aws_iam_role.ecs_task_role.arn
  container_definitions    = templatefile("${path.module}/templates/task.json.tpl", {
    app_name               = var.application
    app_image              = "${aws_ecr_repository.ecr_repo.repository_url}:${var.ecr_tag}@${data.aws_ecr_image.ecr_image.image_digest}"
    app_cpu                = "768"
    app_memory             = "128"
    discord_application_id = aws_secretsmanager_secret.discord_application_id.arn
    discord_public_key     = aws_secretsmanager_secret.discord_public_key.arn
    discord_token          = aws_secretsmanager_secret.discord_token.arn
    discord_guild_id       = aws_secretsmanager_secret.discord_guild_id.arn
    openai_token           = aws_secretsmanager_secret.openai_token.arn
    log_group_name         = aws_cloudwatch_log_group.log_group.name
    region                 = data.aws_region.current.name
    lock_table_name        = aws_dynamodb_table.bot_lock_table.name
  })

  tags = merge(local.tags, {
  })
}

resource "aws_security_group" "ecs_service" {
  name        = "${var.application}-${local.environment}-${random_id.application.hex}-ecs-service"
  description = "Allow outbound traffic from ECS service."
  vpc_id      = aws_vpc.vpc.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_ecs_service" "ecs_service" {
  name            = "${var.application}-${local.environment}-${random_id.application.hex}"
  cluster         = aws_ecs_cluster.ecs_cluster.id
  task_definition = aws_ecs_task_definition.task_definition.arn
  desired_count   = var.scale_down ? 0 : 2
  depends_on      = [
    aws_cloudwatch_log_group.log_group,
    aws_iam_role.ecs_task_role,
  ]
  launch_type     = "EC2"

  triggers = {
    redeployment = timestamp()
  }

  tags = merge(local.tags, {
  })
}

# VPC with two public subnets, IGW, and routing.
resource "aws_vpc" "vpc" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = merge(local.tags, {
    Name = "${var.application}-${local.environment}-${random_id.application.hex}-vpc"
  })
}

resource "aws_internet_gateway" "igw" {
  vpc_id = aws_vpc.vpc.id

  tags = merge(local.tags, {
    Name = "${var.application}-${local.environment}-${random_id.application.hex}-igw"
  })
}

resource "aws_subnet" "public_subnet" {
  count                   = "${var.public_subnet_count}"
  vpc_id                  = aws_vpc.vpc.id
  cidr_block              = "10.0.${count.index}.0/24"
  map_public_ip_on_launch = true
  availability_zone       = data.aws_availability_zones.available.names[count.index]

  tags = merge(local.tags, {
    Name = "${var.application}-${local.environment}-${random_id.application.hex}-public-subnet-${count.index}"
  })
}

resource "aws_route_table" "public_route_table" {
  vpc_id = aws_vpc.vpc.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.igw.id
  }

  tags = merge(local.tags, {
    Name = "${var.application}-${local.environment}-${random_id.application.hex}-public-route-table"
  })
}

resource "aws_route_table_association" "public_route_table_association" {
  count          = "${var.public_subnet_count}"
  subnet_id      = aws_subnet.public_subnet.*.id[count.index]
  route_table_id = aws_route_table.public_route_table.id
}

resource "aws_security_group" "security_group" {
  name        = "${var.application}-${local.environment}-${random_id.application.hex}-security-group"
  description = "Security group for ${var.application}-${local.environment}-${random_id.application.hex}"
  vpc_id      = aws_vpc.vpc.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(local.tags, {
  })
}

# data for latest ECS optimized AMI for t4g.nano
# https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ecs-optimized_AMI.html
data "aws_ami" "ecs-optimized" {
  most_recent = true
  owners = ["amazon"]

  filter {
    name   = "name"
    values = ["amzn2-ami-ecs-hvm-*-arm64-ebs"]
  }
}

# IAM instance role for EC2 instances in ASG. Then attach AmazonEC2ContainerServiceforEC2Role to them.
resource "aws_iam_role" "ecs_instance_role" {
  name               = "${var.application}-${local.environment}-${random_id.application.hex}-ecs-instance-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ec2.amazonaws.com"
        }
      },
    ]
  })
}

resource "aws_iam_role_policy_attachment" "ecs_instance_role_for_ec2" {
  role       = aws_iam_role.ecs_instance_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonEC2ContainerServiceforEC2Role"
}

resource "aws_iam_role_policy_attachment" "ssm_instance_role_for_ec2" {
  role       = aws_iam_role.ecs_instance_role.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "ecs_instance_role" {
  name = aws_iam_role.ecs_instance_role.name
  role = aws_iam_role.ecs_instance_role.name
}

resource "aws_launch_template" "launch_template" {
  name_prefix   = "${var.application}-${local.environment}-${random_id.application.hex}-launch-template"
  image_id      = data.aws_ami.ecs-optimized.id
  iam_instance_profile {
    arn = aws_iam_instance_profile.ecs_instance_role.arn
  }

  network_interfaces {
    delete_on_termination       = true
    device_index                = 0
    security_groups             = [aws_security_group.security_group.id]
    associate_public_ip_address = true
  }

  # User data must include the following line to enable ECS agent
  # https://docs.aws.amazon.com/AmazonECS/latest/developerguide/launch_container_instance.html
  user_data = base64encode(
    templatefile("${path.module}/user_data.sh", {
      cluster_name = aws_ecs_cluster.ecs_cluster.name
    })
  )

  lifecycle {
    create_before_destroy = true
  }

  tags = merge(local.tags, {
  })
}

resource "aws_autoscaling_group" "asg" {
  name                      = "${var.application}-${local.environment}-${random_id.application.hex}"
  max_size                  = var.scale_down ? 0 : 4
  min_size                  = var.scale_down ? 0 : 1
  desired_capacity          = var.scale_down ? 0 : 2
  health_check_grace_period = 120
  health_check_type         = "EC2"
  force_delete              = true
  vpc_zone_identifier       = aws_subnet.public_subnet.*.id
  protect_from_scale_in     = false

  // https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-capacity-rebalancing.html
  capacity_rebalance        = true

  mixed_instances_policy {
    instances_distribution {
      on_demand_base_capacity                  = 0
      on_demand_percentage_above_base_capacity = 0

      // https://aws.amazon.com/blogs/compute/introducing-price-capacity-optimized-allocation-strategy-for-ec2-spot-instances/
      spot_allocation_strategy                 = "price-capacity-optimized"
    }

    launch_template {
      launch_template_specification {
        launch_template_id = aws_launch_template.launch_template.id
        version            = aws_launch_template.launch_template.latest_version
      }

      override {
        instance_type = "t4g.nano"
        weighted_capacity = 1
      }

      // t4g.small are free 750 hours per month until Dec/31/2023
      // See: https://aws.amazon.com/ec2/faqs/#t4g-instances
      override {
        instance_type = "t4g.small"
        weighted_capacity = 1
      }      
    }
  }

  lifecycle {
    create_before_destroy = true
  }

  max_instance_lifetime = 86400

  instance_refresh {
    strategy = "Rolling"
    preferences {
      min_healthy_percentage = 50
    }
  }

  dynamic "tag" {
    for_each = local.tags
    content {
      key                 = tag.key
      value               = tag.value
      propagate_at_launch = true
    }
  }
}

resource "aws_ecs_capacity_provider" "ecs_cluster_provider" {
  name = "${var.application}-${local.environment}-${random_id.application.hex}"

  auto_scaling_group_provider {
    auto_scaling_group_arn = aws_autoscaling_group.asg.arn
    managed_termination_protection = "DISABLED"
    managed_scaling {
      status = "DISABLED"
      target_capacity = 100
      minimum_scaling_step_size = 1
      maximum_scaling_step_size = 10000
      instance_warmup_period = 120
    }
  }

  tags = merge(local.tags, {
  })
}

resource "aws_ecs_cluster_capacity_providers" "ecs_cluster_capacity_providers" {
  cluster_name = aws_ecs_cluster.ecs_cluster.name
  capacity_providers = [aws_ecs_capacity_provider.ecs_cluster_provider.name]

  default_capacity_provider_strategy {
    capacity_provider = aws_ecs_capacity_provider.ecs_cluster_provider.name
    weight            = 100
    base              = 1
  }
}

output "ecr_repository_url" {
  value = aws_ecr_repository.ecr_repo.repository_url
}


output "ecs_log_group" {
    value = aws_cloudwatch_log_group.log_group.name
}
