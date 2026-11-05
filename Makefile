DSN := postgres://test:test@localhost:54321/notes_test?sslmode=disable

.PHONY: up down ps logs test test-compose test-tc test-race run mutants

## up / down: тестовая БД в docker-compose (вариант A)
up:
	docker compose up -d --wait
down:
	docker compose down -v
ps:
	docker compose ps
logs:
	docker compose logs -f

## test-compose: интеграционные тесты на БД из docker-compose (вариант A)
test-compose:
	DB_DSN="$(DSN)" go test -v -count=1 ./integration/...

## test-tc: тесты сами поднимают PostgreSQL через testcontainers-go (вариант B)
test-tc:
	go test -v -count=1 ./integration/...

## test: вариант B по умолчанию (нужен только Docker)
test: test-tc

test-race:
	go test -race -count=1 ./integration/...

## run: API на :8080 поверх БД из docker-compose
run:
	DB_DSN="$(DSN)" go run ./cmd/api

## mutants: проверка, что тесты ловят типичные поломки схемы и репозитория
mutants:
	./scripts/mutants.sh
