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

variable "discord_application_id" {
    description = "Discord application ID"
    type = string
    sensitive = true
}

variable "discord_public_key" {
    description = "Discord public key"
    type = string
    sensitive = true
}

variable "discord_token" {
    description = "Discord token"
    type = string
    sensitive = true
}

variable "discord_guild_id" {
    description = "Discord guild ID"
    type = string
    sensitive = true
}

variable "openai_token" {
    description = "OpenAI API token"
    type = string
    sensitive = true
}
