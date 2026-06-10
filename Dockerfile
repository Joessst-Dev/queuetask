# Build context must be the parent directory of queuetask/ so that the
# queue-ti sibling (go.mod replace directives: ../queue-ti/...) is accessible.
# Local:  docker build -f queuetask/Dockerfile -t queuetask ..
# CI:     handled by release.yml (context: ${{ github.workspace }}/..)
FROM golang:1.25-alpine AS builder

WORKDIR /workspace

# Copy queue-ti modules referenced by go.mod replace directives.
COPY queue-ti/backend/ ./queue-ti/backend/
COPY queue-ti/clients/ ./queue-ti/clients/

WORKDIR /workspace/queuetask
COPY queuetask/go.mod queuetask/go.sum ./
RUN go mod download

COPY queuetask/ .
RUN go build -o /queuetask ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /queuetask /app/queuetask
COPY --from=builder /workspace/queuetask/config.yaml ./
COPY --from=builder /workspace/queuetask/workflows/ ./workflows/

EXPOSE 8081
ENTRYPOINT ["/app/queuetask"]
