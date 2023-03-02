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

resource "aws_secretsmanager_secret" "discord_application_id" {
    name = "/${var.application}/discord_application_id"
}

resource "aws_secretsmanager_secret_version" "discord_application_id" {
    secret_id = aws_secretsmanager_secret.discord_application_id.id
    secret_string = var.discord_application_id
}

resource "aws_secretsmanager_secret" "discord_public_key" {
    name = "/${var.application}/discord_public_key"
}

resource "aws_secretsmanager_secret_version" "discord_public_key" {
    secret_id = aws_secretsmanager_secret.discord_public_key.id
    secret_string = var.discord_public_key
}

resource "aws_secretsmanager_secret" "discord_token" {
    name = "/${var.application}/discord_token"
}

resource "aws_secretsmanager_secret_version" "discord_token" {
    secret_id = aws_secretsmanager_secret.discord_token.id
    secret_string = var.discord_token
}

resource "aws_secretsmanager_secret" "discord_guild_id" {
    name = "/${var.application}/discord_guild_id"
}

resource "aws_secretsmanager_secret_version" "discord_guild_id" {
    secret_id = aws_secretsmanager_secret.discord_guild_id.id
    secret_string = var.discord_guild_id
}

resource "aws_secretsmanager_secret" "openai_token" {
    name = "/${var.application}/openai_token"
}

resource "aws_secretsmanager_secret_version" "openai_token" {
    secret_id = aws_secretsmanager_secret.openai_token.id
    secret_string = var.openai_token
}
