BINARY     := queuetask
BUILD_DIR  := ./bin
MAIN       := ./cmd/server

.PHONY: build run test lint migrate-up migrate-down tidy

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

migrate-up:
	go run $(MAIN) -migrate-only up

migrate-down:
	go run $(MAIN) -migrate-only down
