FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /notifier .

FROM alpine:3.23
RUN apk --no-cache add ca-certificates && \
    addgroup -S notifier && adduser -S -G notifier notifier && \
    mkdir -p /data && chown notifier:notifier /data
COPY --from=builder /notifier /usr/local/bin/notifier
USER notifier
VOLUME ["/data"]
ENTRYPOINT ["notifier"]
