SHELL := /bin/bash
APP_NAME := openai-discord-bot
GO_SRCS := $(shell find src -name '*.go')
TF_SRCS := $(shell find infra -name '*.tf')
AWS_PROFILE := retail-admin
AWS_COMMAND := aws-vault exec $(AWS_PROFILE) --region us-west-2 --

.PHONY: clean

build-bot: $(GO_SRCS)
	cd src && go build -o ../build/$(APP_NAME) main.go

clean:
	rm -rf build/*

run: build-bot
	source env.sh && $(AWS_COMMAND) build/$(APP_NAME)

terraform-init: $(TF_SRCS)
	cd infra && terraform init

terraform-plan: terraform-init
	cd infra && $(AWS_COMMAND) terraform plan

terraform-apply: terraform-init
	cd infra && $(AWS_COMMAND) terraform apply