resource "aws_ecr_repository" "ecr_repo" {
  name                 = "${local.application}-${random_id.application.hex}-${local.environment}"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  tags = merge(local.tags, {
  })
}

resource "aws_cloudwatch_log_group" "log_group" {
  name              = "/aws/ecs/${local.application}-${local.environment}-${random_id.application.hex}"
  retention_in_days = 30

  tags = merge(local.tags, {
  })
}

resource "aws_ecs_cluster" "ecs_cluster" {
  name = "${local.application}-${local.environment}-${random_id.application.hex}"

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

# VPC with two public subnets, IGW, and routing.
resource "aws_vpc" "vpc" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = merge(local.tags, {
    Name = "${local.application}-${local.environment}-${random_id.application.hex}-vpc"
  })
}

resource "aws_internet_gateway" "igw" {
  vpc_id = aws_vpc.vpc.id

  tags = merge(local.tags, {
    Name = "${local.application}-${local.environment}-${random_id.application.hex}-igw"
  })
}

resource "aws_subnet" "public_subnet" {
  count                   = "${var.public_subnet_count}"
  vpc_id                  = aws_vpc.vpc.id
  cidr_block              = "10.0.${count.index}.0/24"
  map_public_ip_on_launch = true
  availability_zone       = data.aws_availability_zones.available.names[count.index]

  tags = merge(local.tags, {
    Name = "${local.application}-${local.environment}-${random_id.application.hex}-public-subnet-${count.index}"
  })
}

resource "aws_route_table" "public_route_table" {
  vpc_id = aws_vpc.vpc.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.igw.id
  }

  tags = merge(local.tags, {
    Name = "${local.application}-${local.environment}-${random_id.application.hex}-public-route-table"
  })
}

resource "aws_route_table_association" "public_route_table_association" {
  count          = "${var.public_subnet_count}"
  subnet_id      = aws_subnet.public_subnet.*.id[count.index]
  route_table_id = aws_route_table.public_route_table.id
}

resource "aws_security_group" "security_group" {
  name        = "${local.application}-${local.environment}-${random_id.application.hex}-security-group"
  description = "Security group for ${local.application}-${local.environment}-${random_id.application.hex}"
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

  filter {
    name   = "name"
    values = ["amzn2-ami-ecs-hvm-*-arm64-ebs"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }

  owners = ["amazon"]
}

resource "aws_launch_template" "launch_template" {
  name_prefix   = "${local.application}-${local.environment}-${random_id.application.hex}-launch-template"
  image_id      = data.aws_ami.ecs-optimized.id
  instance_type = "t4g.nano"

  instance_market_options {
    market_type = "spot"
  }

  ebs_optimized = true

  monitoring {
    enabled = false
  }

  network_interfaces {
    associate_public_ip_address = true
    delete_on_termination       = true
    device_index                = 0
    security_groups             = [aws_security_group.security_group.id]
  }

  tags = merge(local.tags, {
  })
}

resource "aws_autoscaling_group" "asg" {
  name                      = "${local.application}-${local.environment}-${random_id.application.hex}"
  max_size                  = 4
  min_size                  = 1
  desired_capacity          = 1
  health_check_grace_period = 300
  health_check_type         = "EC2"
  force_delete              = true
  vpc_zone_identifier       = aws_subnet.public_subnet.*.id

  launch_template {
    id      = aws_launch_template.launch_template.id
    version = "$Latest"
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
  name = "${local.application}-${local.environment}-${random_id.application.hex}"
  auto_scaling_group_provider {
    auto_scaling_group_arn = aws_autoscaling_group.asg.arn
    managed_termination_protection = "DISABLED"
  }

  tags = merge(local.tags, {
  })
}

resource "aws_ecs_cluster_capacity_providers" "ecs_cluster_capacity_providers" {
  cluster_name = aws_ecs_capacity_provider.ecs_cluster_provider.name
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