FROM golang:1.16.4-alpine AS builder

ARG BUILD_VERSION=0.0.0
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG PROGNAME=redis-rest-gateway
ARG LISTEN_ADDRESS="127.0.0.1"
ARG LISTEN_PORT="8080"

RUN mkdir -p -v /src
WORKDIR /src
ADD . /src

RUN GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build -ldflags="-X 'main.BuildVersion=${BUILD_VERSION}'" -v -o redis-rest-gateway .


FROM alpine:3.13

COPY --from=builder /src/redis-rest-gateway redis-rest-gateway

ENTRYPOINT ["./redis-rest-gateway"]
