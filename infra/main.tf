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
    application = "openai-discord-bot"
    s3_origin_id = "my_s3_origin"
    tags = {
        Application = local.application
    }
}

resource "random_id" "application" {
    byte_length = 8
}

resource "aws_s3_bucket" "my_bucket" {
    bucket = "my-bucket-${random_id.application.hex}"

    tags = merge(local.tags, {
        Name = "my_bucket"
    })
}

resource "aws_s3_bucket_public_access_block" "my_bucket" {
    bucket = aws_s3_bucket.my_bucket.id

    block_public_acls   = true
    block_public_policy = true
    restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "my_bucket" {
    bucket = aws_s3_bucket.my_bucket.id

    rule {
        apply_server_side_encryption_by_default {
            sse_algorithm = "AES256"
        }
    }
}
resource "aws_s3_bucket_acl" "my_bucket_acl" {
    bucket = aws_s3_bucket.my_bucket.id
    acl = "private"
}

resource "aws_cloudfront_origin_access_control" "my_bucket" {
    name = "my_bucket_${random_id.application.hex}"
    description = "my_bucket"
    origin_access_control_origin_type = "s3"
    signing_behavior = "always"
    signing_protocol = "sigv4"
}

resource "aws_cloudfront_distribution" "my_bucket_distribution" {
    origin {
        domain_name = aws_s3_bucket.my_bucket.bucket_regional_domain_name
        origin_access_control_id = aws_cloudfront_origin_access_control.my_bucket.id
        origin_id   = local.s3_origin_id
    }

    enabled             = true
    is_ipv6_enabled     = true
    comment             = "my_bucket"

    default_cache_behavior {
        allowed_methods  = ["GET", "HEAD", "OPTIONS"]
        cached_methods   = ["GET", "HEAD"]
        target_origin_id = local.s3_origin_id

        forwarded_values {
            query_string = false

            cookies {
                forward = "none"
            }
        }

        viewer_protocol_policy = "https-only"
        min_ttl                = 0
        default_ttl            = 3600
        max_ttl                = 86400
    }

    price_class = "PriceClass_100"

    restrictions {
        geo_restriction {
            restriction_type = "none"
        }
    }

    viewer_certificate {
        cloudfront_default_certificate = true
    }

    http_version = "http2and3"

    tags = merge(local.tags, {
        Name = "my_bucket"
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
                    "AWS": ""
                }
            }
        ]
    })

    inline_policy {
        name = "putobject_to_bucket"

        policy = jsonencode({
            Version = "2012-10-17"
            Statement = [
                {
                    Action = "s3:PutObject"
                    Effect = "Allow"
                    Resource = "arn:aws:s3:::${aws_s3_bucket.my_bucket.bucket}/*"
                }
            ]
        })
    }
}

resource "aws_s3_bucket_policy" "my_bucket" {
    bucket = aws_s3_bucket.my_bucket.id
    
    policy = jsonencode({
        Version = "2012-10-17"
        Id = "PolicyForCloudFrontPrivateContent"
        Statement = [
            // Grant CloudFront access to the S3 bucket using a bucket policy
            {
                Sid = "AllowCloudFrontServicePrincipal"
                Effect = "Allow"
                Principal = {
                    "Service" = "cloudfront.amazonaws.com"
                }
                Action = "s3:GetObject"
                Resource = "arn:aws:s3:::${aws_s3_bucket.my_bucket.bucket}/*"
                Condition = {
                    "StringEquals" = {
                        "AWS:SourceArn" = aws_cloudfront_distribution.my_bucket_distribution.arn
                    }
                }
            }
        ]
    })
}

output "my_bucket" {
    value = aws_s3_bucket.my_bucket.bucket
}

output "my_bucket_distribution" {
    value = aws_cloudfront_distribution.my_bucket_distribution.domain_name
}

output "my_bucket_distribution_id" {
    value = aws_cloudfront_distribution.my_bucket_distribution.id
}

output "my_bucket_distribution_arn" {
    value = aws_cloudfront_distribution.my_bucket_distribution.arn
}

output "bot_role" {
    value = aws_iam_role.bot_role.arn
}