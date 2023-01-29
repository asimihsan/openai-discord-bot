# syntax=docker/dockerfile:1

FROM golang:1.19.5-bullseye AS builder

RUN \
    --mount=type=cache,target=/var/cache/apt \
    apt-get update && \
    apt-get install -y ca-certificates git 

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