package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/akmalabdulatifzoda-gif/pz-16/internal/db"
	"github.com/akmalabdulatifzoda-gif/pz-16/internal/httpapi"
	"github.com/akmalabdulatifzoda-gif/pz-16/internal/repo"
	"github.com/akmalabdulatifzoda-gif/pz-16/internal/service"
)

// env — изолированное окружение одного теста: своя схема PostgreSQL,
// применённые миграции, пул соединений и HTTP-сервер приложения.
type env struct {
	t      *testing.T
	Pool   *pgxpool.Pool
	Server *httptest.Server
	Schema string
}

// newEnv создаёт схему test_<random>, применяет миграции и запускает API.
// В t.Cleanup схема удаляется целиком (DROP SCHEMA ... CASCADE), поэтому
// тесты не видят данных друг друга и могут выполняться параллельно и в любом порядке.
func newEnv(t *testing.T) *env {
	t.Helper()
	if baseDSN == "" {
		t.Skip("нет тестовой БД: задайте DB_DSN или запустите Docker")
	}
	ctx := context.Background()

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "test_" + hex.EncodeToString(b)

	admin, err := db.Connect(ctx, baseDSN, "")
	require.NoError(t, err)
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		if err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
		admin.Close()
	})

	pool, err := db.Connect(ctx, baseDSN, schema)
	require.NoError(t, err)
	applied, err := db.ApplyMigrations(ctx, pool)
	require.NoError(t, err, "миграции должны примениться на чистую схему")
	require.Equal(t, []int{1, 2}, applied)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	svc := &service.Service{Notes: repo.NoteRepo{DB: pool}}
	srv := httptest.NewServer(httpapi.Router{Svc: svc, Ping: pool.Ping, Log: logger}.Handler())
	t.Cleanup(func() { srv.Close(); pool.Close() }) // выполнится раньше DROP SCHEMA (LIFO)

	return &env{t: t, Pool: pool, Server: srv, Schema: schema}
}

// do выполняет HTTP-запрос к API и возвращает статус, заголовки и тело.
func (e *env) do(method, path, body string) (int, http.Header, []byte) {
	e.t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.Server.URL+path, r)
	require.NoError(e.t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(e.t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(e.t, err)
	return resp.StatusCode, resp.Header, b
}

// doJSON — do + разбор JSON-ответа в out.
func (e *env) doJSON(method, path, body string, wantStatus int, out any) http.Header {
	e.t.Helper()
	status, h, b := e.do(method, path, body)
	require.Equalf(e.t, wantStatus, status, "%s %s: тело ответа %s", method, path, b)
	if out != nil {
		require.Contains(e.t, h.Get("Content-Type"), "application/json")
		require.NoError(e.t, json.Unmarshal(b, out), "ответ должен быть корректным JSON: %s", b)
	}
	return h
}

// count — число строк в таблице текущей схемы (проверка эффектов в БД).
func (e *env) count(table string, where string, args ...any) int {
	e.t.Helper()
	q := "SELECT count(*) FROM " + table
	if where != "" {
		q += " WHERE " + where
	}
	var n int
	require.NoError(e.t, e.Pool.QueryRow(context.Background(), q, args...).Scan(&n))
	return n
}

// loadFixture выполняет SQL-файл из testdata/fixtures в схеме теста.
func (e *env) loadFixture(name string) {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", "fixtures", name))
	require.NoError(e.t, err)
	_, err = e.Pool.Exec(context.Background(), string(b))
	require.NoError(e.t, err, "фикстура %s", name)
}
