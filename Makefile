SHELL := /bin/bash
APP_NAME := openai-discord-bot
SRCS := $(shell find src -name '*.go')

.PHONY: clean

build-bot: $(SRCS)
	cd src && go build -o ../build/$(APP_NAME) main.go

clean:
	rm -rf build/*

run: build-bot
	source env.sh && build/$(APP_NAME)