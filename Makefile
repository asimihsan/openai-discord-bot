SHELL := /bin/bash
APP_NAME := openai-discord-bot
GO_SRCS := $(shell find src -name '*.go')
TF_SRCS := $(shell find infra -name '*.tf')
AWS_PROFILE := retail-admin
AWS_COMMAND := aws-vault exec $(AWS_PROFILE) --region us-west-2 --

.PHONY: clean

build-bot: $(GO_SRCS)
	cd src && docker buildx build --platform linux/arm64 -t $(APP_NAME) .

clean:
	rm -rf build/*

run: build-bot
	source env.sh && $(AWS_COMMAND) docker run \
		--platform linux/arm64 \
		-e DISCORD_APPLICATION_ID \
		-e DISCORD_PUBLIC_KEY \
		-e DISCORD_TOKEN \
		-e DISCORD_GUILD_ID \
		-e OPENAI_TOKEN \
		-e LOCK_TABLE_NAME \
		-e AWS_REGION \
		-e AWS_ACCESS_KEY_ID \
		-e AWS_SECRET_ACCESS_KEY \
		-e AWS_SESSION_TOKEN \
		--rm -it $(APP_NAME)


terraform-init: $(TF_SRCS)
	cd infra && terraform init

terraform-plan: terraform-init
	cd infra && $(AWS_COMMAND) terraform plan

terraform-apply: terraform-init
	cd infra && $(AWS_COMMAND) terraform apply