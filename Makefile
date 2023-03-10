SHELL := /bin/bash
APP_NAME := openai-discord-bot
GO_SRCS := $(shell find src -name '*.go')
TF_SRCS := $(shell find infra -name '*.tf')
AWS_PROFILE := retail-admin
AWS_REGION := us-west-2
AWS_COMMAND := aws-vault exec $(AWS_PROFILE) --region $(AWS_REGION) --

.PHONY: clean

init:
	brew install aws-vault
	brew install awscli
	brew install docker-credential-helper-ecr
	brew install jq
	brew install sops

build-bot: $(GO_SRCS)
	cd src && docker buildx build --platform linux/arm64 -t $(APP_NAME) .

build-push-image:
	$(AWS_COMMAND) ./infra/build-push-image.sh latest

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
	cd infra && $(AWS_COMMAND) terraform plan -var-file=secret-variables.tfvars

terraform-apply: terraform-init
	cd infra && $(AWS_COMMAND) terraform apply -var-file=secret-variables.tfvars

terraform-apply-scale-down: terraform-init
	cd infra && $(AWS_COMMAND) terraform apply -var-file=secret-variables.tfvars -var="scale_down=true"

list-log-groups:
	$(AWS_COMMAND) cwl-mount --region $(AWS_REGION) list-log-groups

mount-ecs-log-group:
	$(AWS_COMMAND) ./infra/mount-ecs-log-group.sh
