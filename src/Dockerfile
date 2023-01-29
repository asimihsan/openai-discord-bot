# syntax=docker/dockerfile:1

FROM golang:1.19.5-bullseye AS builder

RUN \
    --mount=type=cache,target=/var/cache/apt \
    apt-get update && \
    apt-get install -y ca-certificates git 

# FROM golang:1.19.5-alpine AS builder
# RUN \
#     --mount=type=cache,target=/var/cache/apk \
#     ln -s /var/cache/apk /etc/apk/cache && \
#     apk update && \
#     apk add --no-cache git

WORKDIR /go/src/hello
COPY . .
RUN go mod download
RUN go build -o /godocker

FROM debian:bullseye-slim

RUN \
    --mount=type=cache,target=/var/cache/apt \
    apt-get update && \
    apt-get install -y ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /godocker /godocker
ENTRYPOINT ["/godocker"]