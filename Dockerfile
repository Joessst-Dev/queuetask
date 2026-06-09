FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /queuetask ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /queuetask /app/queuetask
COPY config.yaml ./
COPY workflows/ ./workflows/

EXPOSE 8081
ENTRYPOINT ["/app/queuetask"]
