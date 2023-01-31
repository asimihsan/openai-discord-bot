data "aws_region" "current" {}

data "aws_availability_zones" "available" {
    // Exclude local zones
    filter {
        name   = "opt-in-status"
        values = ["opt-in-not-required"]
    }
}