FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# VERSION is injected into main.version so the running notifier reports its
# build to the relay. Pass at build: --build-arg VERSION=$(git describe --tags --always)
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.version=${VERSION}" -o /notifier .

FROM alpine:3.23
RUN apk --no-cache add ca-certificates && \
    addgroup -S notifier && adduser -S -G notifier notifier && \
    mkdir -p /data && chown notifier:notifier /data
COPY --from=builder /notifier /usr/local/bin/notifier
USER notifier
VOLUME ["/data"]
ENTRYPOINT ["notifier"]
