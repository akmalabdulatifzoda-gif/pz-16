// Package integration — интеграционные тесты API на настоящем PostgreSQL:
// HTTP → хендлер → сервис → репозиторий → БД.
//
// Два способа получить базу:
//
//	A. docker-compose: make up && DB_DSN=... go test ./integration/...
//	B. testcontainers-go: если DB_DSN не задан, тесты сами поднимают контейнер
//	   postgres:16-alpine и удаляют его после прогона.
package integration

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	baseDSN string // DSN общей тестовой БД; у каждого теста внутри неё своя схема
	infra   string // "docker-compose" или "testcontainers" — для лога
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if dsn := os.Getenv("DB_DSN"); dsn != "" {
		baseDSN, infra = dsn, "docker-compose (DB_DSN)"
		log.Printf("integration: используем внешнюю БД из DB_DSN")
		return m.Run()
	}

	ctx := context.Background()
	start := time.Now()
	pg, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("notes_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2). // первый раз — во время initdb, второй — рабочий старт
				WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		log.Printf("integration: не удалось запустить контейнер PostgreSQL: %v", err)
		if os.Getenv("CI") != "" {
			return 1 // в CI отсутствие Docker — ошибка, а не пропуск
		}
		log.Printf("integration: тесты пропущены (нет Docker и не задан DB_DSN)")
		return 0
	}
	defer func() {
		if err := testcontainers.TerminateContainer(pg); err != nil {
			log.Printf("integration: terminate: %v", err)
		}
	}()

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Printf("integration: connection string: %v", err)
		return 1
	}
	baseDSN = dsn
	infra = fmt.Sprintf("testcontainers (%s, старт %.1fs)", pg.GetContainerID()[:12], time.Since(start).Seconds())
	log.Printf("integration: контейнер PostgreSQL готов: %s", infra)
	return m.Run()
}
