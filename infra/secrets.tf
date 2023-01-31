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
