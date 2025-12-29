.PHONY: help build start stop logs clean local build-local

help:
	@echo "Available targets:"
	@echo "  build       - Build Docker image"
	@echo "  start       - Start the bot container"
	@echo "  stop        - Stop the bot container"
	@echo "  restart     - Restart the bot container"
	@echo "  logs        - View container logs"
	@echo "  logs-f      - Follow container logs"
	@echo "  clean       - Remove container and image"
	@echo "  shell       - Open shell in running container"
	@echo "  build-local - Build binary locally"
	@echo "  local       - Build and run locally (no Docker)"

## build: builds the Docker image
build:
	@echo "Building Docker image..."
	docker compose build
	@echo "Image built!"

## start: starts the bot container
start:
	@echo "Starting the bot..."
	docker compose up -d
	@echo "Bot running!"

## stop: stops the bot container
stop:
	@echo "Stopping the bot..."
	docker compose down
	@echo "Bot stopped!"

## restart: restarts the bot container
restart: stop start

## logs: view container logs
logs:
	docker compose logs

## logs-f: follow container logs
logs-f:
	docker compose logs -f

## clean: remove container and image
clean:
	@echo "Cleaning up..."
	docker compose down --rmi local
	@echo "Cleaned!"

## shell: open shell in running container
shell:
	docker compose exec xephyr /bin/sh

## build-local: builds the binary locally
build-local:
	@echo "Building binary..."
	@mkdir -p dist
	@go build -o dist/Xephyr ./cmd
	@echo "Binary built: dist/Xephyr"

## local: builds and runs locally (no Docker)
local: build-local
	@echo "Starting bot locally..."
	@set -a && . ./.env && set +a && ./dist/Xephyr
