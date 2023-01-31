variable "public_subnet_count" {
    description = "Number of public subnets to create"
    type = number
    default = 2
}

variable "application" {
    description = "Name of the application"
    type = string
    default = "openai-discord-bot"
}