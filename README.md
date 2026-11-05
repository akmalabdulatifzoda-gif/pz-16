# ПЗ №16 — Интеграционное тестирование API. Docker для тестовой БД

**Студент:** Абдулатифзода Акмал, группа ЭФМО-0126
**Дисциплина:** Технологии индустриального программирования (преподаватель Адышкин С. С.)

![integration](https://github.com/akmalabdulatifzoda-gif/pz-16/actions/workflows/integration.yml/badge.svg)

Интеграционные тесты Notes API по всей цепочке
**HTTP → хендлер → сервис → репозиторий → настоящий PostgreSQL 16 в Docker**.
Поддержаны оба подхода из задания: **A — docker-compose**, **B — testcontainers-go**.

## Структура

```
cmd/api/main.go               API (chi + pgxpool), миграции при старте
internal/models/note.go       модель, DTO, доменные ошибки
internal/db/migrate.go        подключение, встроенные (go:embed) версионные миграции
internal/db/migrations/       001_notes.sql (CHECK, триггер updated_at, индекс), 002_tags.sql (FK CASCADE, PK)
internal/repo/postgres.go     SQL: транзакция заметка+теги, keyset-пагинация, фильтры
internal/service/service.go   валидация, нормализация, лимиты
internal/httpapi/handlers.go  маршруты, строгий JSON, коды ответов, логирование ошибок
integration/                  интеграционные тесты (TestMain выбирает A или B)
testdata/fixtures/notes.sql   фикстура на 25 заметок
scripts/mutants.sh            мутационная проверка тестов
.github/workflows/            CI: оба варианта
```

## Запуск

**Вариант B — testcontainers-go** (нужен только запущенный Docker):

```bash
go test -v ./integration/...      # контейнер postgres:16-alpine поднимется и удалится сам
```

**Вариант A — docker-compose:**

```bash
make up            # docker compose up -d --wait (порт 54321, healthcheck)
make ps            # docker compose ps
make test-compose  # DB_DSN=postgres://test:test@localhost:54321/notes_test go test -v ./integration/...
make down          # docker compose down -v
```

Если `DB_DSN` задан — тесты используют эту БД (вариант A), иначе поднимают контейнер (вариант B).
Без Docker и без `DB_DSN` тесты пропускаются локально, а в CI (`CI=true`) — падают.

Прочее: `make run` — API на :8080 поверх БД из compose; `make test-race`; `make mutants`.

## Как обеспечена независимость тестов

Каждый тест создаёт **собственную схему** `test_<random>` (search_path пула), применяет в ней миграции,
при необходимости грузит фикстуру и в `t.Cleanup` удаляет схему целиком (`DROP SCHEMA ... CASCADE`).
Поэтому все тесты выполняются с `t.Parallel()`, в любом порядке и не видят данных друг друга.

## Что проверяется (14 тестов, 43 проверки с подтестами)

CRUD с проверкой статусов, заголовков, JSON и эффектов в БД; 404/400 и валидация (8 видов плохого POST);
откат транзакции при ошибке в тегах; триггер `updated_at`; каскадное удаление тегов; keyset-пагинация без
пропусков и дублей (и стабильность при вставке между страницами); фильтры по подстроке и тегу; параллельные
POST и PATCH; ограничения схемы напрямую SQL; идемпотентность миграций.
Мутационная проверка (`results/mutants.txt`): 5 из 5 намеренных поломок пойманы.

Отчёт и скриншоты — в [report/](report).
