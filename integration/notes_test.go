package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akmalabdulatifzoda-gif/pz-16/internal/db"
	"github.com/akmalabdulatifzoda-gif/pz-16/internal/models"
)

type errBody struct {
	Error string `json:"error"`
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var got map[string]string
	e.doJSON("GET", "/healthz", "", http.StatusOK, &got)
	assert.Equal(t, "ok", got["status"])
	t.Logf("инфраструктура: %s, схема %s", infra, e.Schema)
}

func TestCreateNote(t *testing.T) {
	t.Parallel()
	e := newEnv(t)

	var n models.Note
	h := e.doJSON("POST", "/notes", `{"title":"  Hello  ","content":"World","tags":["Go","db","go"]}`, http.StatusCreated, &n)

	// Ответ
	assert.Positive(t, n.ID)
	assert.Equal(t, "Hello", n.Title, "заголовок обрезан от пробелов")
	assert.Equal(t, "World", n.Content)
	assert.Equal(t, []string{"db", "go"}, n.Tags, "теги в нижнем регистре, дубликаты схлопнуты")
	assert.WithinDuration(t, time.Now(), n.CreatedAt, time.Minute)
	assert.Equal(t, n.CreatedAt, n.UpdatedAt)
	assert.Equal(t, fmt.Sprintf("/notes/%d", n.ID), h.Get("Location"))

	// Эффект в БД
	var title, content string
	require.NoError(t, e.Pool.QueryRow(context.Background(),
		`SELECT title, content FROM notes WHERE id = $1`, n.ID).Scan(&title, &content))
	assert.Equal(t, "Hello", title)
	assert.Equal(t, "World", content)
	assert.Equal(t, 2, e.count("note_tags", "note_id = $1", n.ID))
}

func TestCreateNote_Validation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	cases := []struct {
		name, body, wantErr string
	}{
		{"битый JSON", `{"title":`, "bad json"},
		{"не объект", `["title"]`, "bad json"},
		{"неизвестное поле", `{"title":"x","author":"me"}`, "unknown field"},
		{"мусор после JSON", `{"title":"x"} {"title":"y"}`, "unexpected data"},
		{"пустой заголовок", `{"title":""}`, "title is required"},
		{"заголовок из пробелов", `{"title":"   "}`, "title is required"},
		{"заголовок длиннее 200", `{"title":"` + strings.Repeat("я", 201) + `"}`, "longer than 200"},
		{"неверный формат тега (CHECK в БД)", `{"title":"ok","tags":["bad tag!"]}`, "note_tags_tag_check"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var eb errBody
			e.doJSON("POST", "/notes", c.body, http.StatusBadRequest, &eb)
			assert.Contains(t, eb.Error, c.wantErr)
		})
	}
	assert.Zero(t, e.count("notes", ""), "ни один некорректный запрос не должен оставить строк в БД")
}

// Тег нарушает CHECK уже после вставки заметки — транзакция должна откатить и заметку.
func TestCreateNote_TransactionRollback(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	status, _, body := e.do("POST", "/notes", `{"title":"будет откат","tags":["ok", "`+strings.Repeat("x", 31)+`"]}`)
	require.Equal(t, http.StatusBadRequest, status, string(body))
	assert.Zero(t, e.count("notes", ""), "заметка не сохранилась")
	assert.Zero(t, e.count("note_tags", ""), "теги не сохранились")

	// Последовательность id при этом «сгорела» — это нормальное поведение PostgreSQL.
	var n models.Note
	e.doJSON("POST", "/notes", `{"title":"после отката"}`, http.StatusCreated, &n)
	assert.Equal(t, int64(2), n.ID)
}

func TestGetNote(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var created models.Note
	e.doJSON("POST", "/notes", `{"title":"Read me","content":"body","tags":["go"]}`, http.StatusCreated, &created)

	t.Run("существующая", func(t *testing.T) {
		var got models.Note
		e.doJSON("GET", fmt.Sprintf("/notes/%d", created.ID), "", http.StatusOK, &got)
		assert.Equal(t, created, got)
	})
	t.Run("несуществующая — 404", func(t *testing.T) {
		var eb errBody
		e.doJSON("GET", "/notes/999999", "", http.StatusNotFound, &eb)
		assert.Equal(t, "not found", eb.Error)
	})
	for _, bad := range []string{"abc", "0", "-5", "1.5"} {
		t.Run("некорректный id "+bad, func(t *testing.T) {
			var eb errBody
			e.doJSON("GET", "/notes/"+bad, "", http.StatusBadRequest, &eb)
			assert.Equal(t, "invalid id", eb.Error)
		})
	}
}

func TestUpdateNote(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var n models.Note
	e.doJSON("POST", "/notes", `{"title":"Old","content":"keep me"}`, http.StatusCreated, &n)
	path := fmt.Sprintf("/notes/%d", n.ID)

	t.Run("меняется только переданное поле, updated_at выставляет триггер", func(t *testing.T) {
		var u models.Note
		e.doJSON("PATCH", path, `{"title":"New"}`, http.StatusOK, &u)
		assert.Equal(t, "New", u.Title)
		assert.Equal(t, "keep me", u.Content, "content не передавали — не изменился")
		assert.Equal(t, n.CreatedAt, u.CreatedAt)
		assert.True(t, u.UpdatedAt.After(n.UpdatedAt), "updated_at обновлён триггером БД")

		var dbTitle string
		var dbUpdated time.Time
		require.NoError(t, e.Pool.QueryRow(context.Background(),
			`SELECT title, updated_at FROM notes WHERE id = $1`, n.ID).Scan(&dbTitle, &dbUpdated))
		assert.Equal(t, "New", dbTitle)
		assert.True(t, dbUpdated.Equal(u.UpdatedAt))
	})
	t.Run("пустой PATCH — 400", func(t *testing.T) {
		var eb errBody
		e.doJSON("PATCH", path, `{}`, http.StatusBadRequest, &eb)
		assert.Contains(t, eb.Error, "nothing to update")
	})
	t.Run("пустой заголовок — 400, данные не меняются", func(t *testing.T) {
		e.doJSON("PATCH", path, `{"title":" "}`, http.StatusBadRequest, nil)
		assert.Equal(t, 1, e.count("notes", "title = 'New'"))
	})
	t.Run("несуществующая — 404", func(t *testing.T) {
		e.doJSON("PATCH", "/notes/999999", `{"title":"x"}`, http.StatusNotFound, nil)
	})
}

func TestDeleteNote(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var n models.Note
	e.doJSON("POST", "/notes", `{"title":"Temp","tags":["a","b"]}`, http.StatusCreated, &n)
	require.Equal(t, 2, e.count("note_tags", "note_id = $1", n.ID))
	path := fmt.Sprintf("/notes/%d", n.ID)

	status, _, body := e.do("DELETE", path, "")
	require.Equal(t, http.StatusNoContent, status)
	assert.Empty(t, body, "у 204 нет тела")

	assert.Zero(t, e.count("notes", "id = $1", n.ID), "строка удалена")
	assert.Zero(t, e.count("note_tags", "note_id = $1", n.ID), "теги удалены каскадно (ON DELETE CASCADE)")
	e.doJSON("GET", path, "", http.StatusNotFound, nil)
	e.doJSON("DELETE", path, "", http.StatusNotFound, nil)
}

func TestListNotes_Pagination(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.loadFixture("notes.sql")

	var all []int64
	cursor, pages := "", 0
	for {
		path := "/notes?limit=10"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		var p models.Page
		e.doJSON("GET", path, "", http.StatusOK, &p)
		pages++
		require.LessOrEqual(t, len(p.Items), 10)
		for _, n := range p.Items {
			all = append(all, n.ID)
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
		require.Less(t, pages, 10, "пагинация не должна зацикливаться")
	}
	assert.Equal(t, 3, pages, "25 записей по 10 = 3 страницы")
	require.Len(t, all, 25, "без пропусков")
	for i := 1; i < len(all); i++ {
		require.Less(t, all[i], all[i-1], "строгий порядок id DESC, без дублей")
	}
	assert.Equal(t, int64(25), all[0])
	assert.Equal(t, int64(1), all[24])
}

func TestListNotes_PaginationStableOnInsert(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.loadFixture("notes.sql")

	var p1 models.Page
	e.doJSON("GET", "/notes?limit=10", "", http.StatusOK, &p1)
	// Между страницами кто-то добавил заметку: keyset не сдвигает следующую страницу.
	e.doJSON("POST", "/notes", `{"title":"новая между страницами"}`, http.StatusCreated, nil)
	var p2 models.Page
	e.doJSON("GET", "/notes?limit=10&cursor="+p1.NextCursor, "", http.StatusOK, &p2)
	assert.Equal(t, int64(15), p2.Items[0].ID, "вторая страница начинается сразу после первой")
}

func TestListNotes_Filters(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.loadFixture("notes.sql")

	cases := []struct {
		name, query string
		wantCount   int
		check       func(t *testing.T, n models.Note)
	}{
		{"по подстроке, без учёта регистра", "q=REDIS&limit=100", 10,
			func(t *testing.T, n models.Note) { assert.Contains(t, n.Title, "Redis") }},
		{"по тегу go", "tag=go&limit=100", 5,
			func(t *testing.T, n models.Note) { assert.Contains(t, n.Tags, "go") }},
		{"по тегу db", "tag=db&limit=100", 8,
			func(t *testing.T, n models.Note) { assert.Contains(t, n.Tags, "db") }},
		{"оба фильтра", "q=redis&tag=db&limit=100", 3, // id 3, 6, 9
			func(t *testing.T, n models.Note) {
				assert.Contains(t, n.Tags, "db")
				assert.LessOrEqual(t, n.ID, int64(10))
			}},
		{"ничего не найдено", "q=kubernetes", 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var p models.Page
			e.doJSON("GET", "/notes?"+c.query, "", http.StatusOK, &p)
			require.Len(t, p.Items, c.wantCount)
			assert.Empty(t, p.NextCursor)
			for _, n := range p.Items {
				c.check(t, n)
			}
		})
	}
	t.Run("пагинация внутри фильтра", func(t *testing.T) {
		var p1, p2 models.Page
		e.doJSON("GET", "/notes?tag=db&limit=5", "", http.StatusOK, &p1)
		require.Len(t, p1.Items, 5)
		require.NotEmpty(t, p1.NextCursor)
		e.doJSON("GET", "/notes?tag=db&limit=5&cursor="+p1.NextCursor, "", http.StatusOK, &p2)
		assert.Len(t, p2.Items, 3)
		assert.Empty(t, p2.NextCursor)
	})
	for _, bad := range []string{"limit=0", "limit=101", "limit=abc", "cursor=-1", "cursor=x"} {
		t.Run("некорректный параметр "+bad, func(t *testing.T) {
			e.doJSON("GET", "/notes?"+bad, "", http.StatusBadRequest, nil)
		})
	}
}

// Параллельные POST: все заметки сохраняются, id уникальны.
func TestConcurrentCreates(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const n = 50
	ids := make(chan int64, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var note models.Note
			e.doJSON("POST", "/notes", fmt.Sprintf(`{"title":"parallel %d","tags":["load"]}`, i), http.StatusCreated, &note)
			ids <- note.ID
		}(i)
	}
	wg.Wait()
	close(ids)
	seen := map[int64]bool{}
	for id := range ids {
		assert.False(t, seen[id], "id %d повторился", id)
		seen[id] = true
	}
	assert.Len(t, seen, n)
	assert.Equal(t, n, e.count("notes", ""))
	assert.Equal(t, n, e.count("note_tags", "tag = 'load'"))
}

// Конкурентные PATCH одной заметки: каждый запрос атомарен, итог — одно из записанных значений.
func TestConcurrentUpdates(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var n models.Note
	e.doJSON("POST", "/notes", `{"title":"start"}`, http.StatusCreated, &n)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e.doJSON("PATCH", fmt.Sprintf("/notes/%d", n.ID), fmt.Sprintf(`{"content":"v%d"}`, i), http.StatusOK, nil)
		}(i)
	}
	wg.Wait()
	var got models.Note
	e.doJSON("GET", fmt.Sprintf("/notes/%d", n.ID), "", http.StatusOK, &got)
	assert.Regexp(t, `^v([0-9]|1[0-9])$`, got.Content)
	assert.Equal(t, "start", got.Title)
}

// Ограничения схемы проверяем и напрямую SQL — они защищают данные даже в обход API.
func TestSchemaConstraints(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.Pool.Exec(ctx, `INSERT INTO notes (title) VALUES ('')`)
	assert.ErrorContains(t, err, "notes_title_check")
	_, err = e.Pool.Exec(ctx, `INSERT INTO note_tags (note_id, tag) VALUES (12345, 'go')`)
	assert.ErrorContains(t, err, "note_tags_note_id_fkey", "тег без заметки нарушает внешний ключ")
	var id int64
	require.NoError(t, e.Pool.QueryRow(ctx, `INSERT INTO notes (title) VALUES ('x') RETURNING id`).Scan(&id))
	_, err = e.Pool.Exec(ctx, `INSERT INTO note_tags (note_id, tag) VALUES ($1, 'go'), ($1, 'go')`, id)
	assert.ErrorContains(t, err, "note_tags_pkey", "повтор тега у одной заметки запрещён")
}

func TestMigrationsIdempotent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	applied, err := db.ApplyMigrations(context.Background(), e.Pool)
	require.NoError(t, err)
	assert.Empty(t, applied, "повторный запуск ничего не применяет")
	assert.Equal(t, 2, e.count("schema_migrations", ""))
}
