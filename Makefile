SHELL := /bin/zsh

COMPOSE := docker compose

.PHONY: up down restart ps logs run stop db-up db-down
.PHONY: migrate-up migrate-down proto env

up:
	$(COMPOSE) up -d

down:
	$(COMPOSE) down

restart:
	$(COMPOSE) down
	$(COMPOSE) up -d

ps:
	$(COMPOSE) ps

logs:
	$(COMPOSE) logs -f --tail=200

run: up

stop: down

db-up:
	$(COMPOSE) up -d postgres

db-down:
	$(COMPOSE) stop postgres

migrate-up:
	@echo "TODO: подключить goose migrate up"

migrate-down:
	@echo "TODO: подключить goose migrate down"

proto:
	@echo "TODO: подключить protoc генерацию"

env:
	@test -f .env || cp .env.example .env
	@echo ".env готов"
