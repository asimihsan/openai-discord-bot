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

variable "ecr_tag" {
    description = "ECR tag"
    type = string
    default = "latest"
}

variable "scale_down" {
    description = "Scale down the cluster"
    type = bool
    default = false
}

variable "public_subnet_count" {
    description = "Number of public subnets to create"
    type = number
    default = 3
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
