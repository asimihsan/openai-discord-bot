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

terraform {
    required_version = ">= 0.13"
    required_providers {
        aws = {
            source  = "hashicorp/aws"
            version = "~> 4.0"
        }
        random = {
            source  = "hashicorp/random"
            version = "~> 3.0"
        }
    }
}

provider "aws" {
  region = "us-west-2"
}

locals {
    environment = "production"
    tags = {
        Application = var.application
        Environment = local.environment
    }
}

resource "random_id" "application" {
    byte_length = 8
}

// DynamoDB lock table
resource "aws_dynamodb_table" "bot_lock_table" {
    name = "bot_lock_table_${random_id.application.hex}"
    billing_mode = "PAY_PER_REQUEST"
    
    server_side_encryption {
        enabled = true
    }

    hash_key = "LockID"

    attribute {
        name = "LockID"
        type = "S"
    }

    attribute {
        name = "LastUpdated"
        type = "N"
    }

    attribute {
        name = "Shard"
        type = "N"
    }

    global_secondary_index {
        name = "ShardIndex"
        hash_key = "Shard"
        range_key = "LastUpdated"
        projection_type = "ALL"
    }

    ttl {
        attribute_name = "TTL"
        enabled = true
    }

    tags = merge(local.tags, {
        Name = "bot_lock_table"
    })
}

resource "aws_iam_role" "bot_role" {
    name = "bot_role_${random_id.application.hex}"

    assume_role_policy = jsonencode({
        Version = "2012-10-17"
        Statement = [
            {
                Action = "sts:AssumeRole"
                Effect = "Allow"
                Principal = {
                    Service = "ec2.amazonaws.com"
                }
            }
        ]
    })
}

output "aws_region" {
    value = data.aws_region.current.name
}

output "bot_role" {
    value = aws_iam_role.bot_role.arn
}

output "bot_lock_table" {
    value = aws_dynamodb_table.bot_lock_table.name
}