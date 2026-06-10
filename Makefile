BINARY     := queuetask
BUILD_DIR  := ./bin
MAIN       := ./cmd/server
IMAGE      := queuetask

.PHONY: build run test lint migrate-up migrate-down tidy docker-build

build:
	go build -o $(BUILD_DIR)/$(BINARY) $(MAIN)

run:
	go run $(MAIN)

test:
	go test ./... -v

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

# Docker build requires parent dir as context (queue-ti replace directives).
docker-build:
	cd .. && docker build -f queuetask/Dockerfile -t $(IMAGE) .

migrate-up:
	go run $(MAIN) -migrate-only up

migrate-down:
	go run $(MAIN) -migrate-only down
