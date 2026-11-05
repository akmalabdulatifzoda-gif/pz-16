// Package repo — доступ к заметкам в PostgreSQL.
package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akmalabdulatifzoda-gif/pz-16/internal/models"
)

type NoteRepo struct{ DB *pgxpool.Pool }

// mapErr переводит ошибки ограничений БД в доменные ошибки.
func mapErr(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23514": // check_violation
			return fmt.Errorf("%w: constraint %s", models.ErrValidation, pg.ConstraintName)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: %s", models.ErrNotFound, pg.ConstraintName)
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return models.ErrNotFound
	}
	return err
}

// Create вставляет заметку и её теги в одной транзакции:
// если хотя бы один тег нарушает ограничение, не сохраняется ничего.
func (r NoteRepo) Create(ctx context.Context, n *models.Note) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit — no-op

	if err := tx.QueryRow(ctx,
		`INSERT INTO notes (title, content) VALUES ($1, $2) RETURNING id, created_at, updated_at`,
		n.Title, n.Content,
	).Scan(&n.ID, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return mapErr(err)
	}
	if len(n.Tags) > 0 {
		// ON CONFLICT DO NOTHING — повторяющиеся теги схлопываются по первичному ключу.
		if _, err := tx.Exec(ctx,
			`INSERT INTO note_tags (note_id, tag) SELECT $1, unnest($2::text[]) ON CONFLICT DO NOTHING`,
			n.ID, n.Tags); err != nil {
			return mapErr(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	tags, err := r.tags(ctx, n.ID)
	n.Tags = tags
	return err
}

func (r NoteRepo) tags(ctx context.Context, id int64) ([]string, error) {
	rows, err := r.DB.Query(ctx, `SELECT tag FROM note_tags WHERE note_id = $1 ORDER BY tag`, id)
	if err != nil {
		return nil, err
	}
	tags, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if tags == nil {
		tags = []string{}
	}
	return tags, err
}

const selectNote = `
	SELECT n.id, n.title, n.content, n.created_at, n.updated_at,
	       COALESCE(array_agg(t.tag ORDER BY t.tag) FILTER (WHERE t.tag IS NOT NULL), '{}') AS tags
	FROM notes n
	LEFT JOIN note_tags t ON t.note_id = n.id`

func scan(row pgx.Row) (models.Note, error) {
	var n models.Note
	err := row.Scan(&n.ID, &n.Title, &n.Content, &n.CreatedAt, &n.UpdatedAt, &n.Tags)
	return n, err
}

func (r NoteRepo) Get(ctx context.Context, id int64) (models.Note, error) {
	n, err := scan(r.DB.QueryRow(ctx, selectNote+` WHERE n.id = $1 GROUP BY n.id`, id))
	return n, mapErr(err)
}

// List — keyset-пагинация по id DESC с фильтрами по подстроке заголовка и тегу.
func (r NoteRepo) List(ctx context.Context, f models.ListFilter) ([]models.Note, error) {
	rows, err := r.DB.Query(ctx, selectNote+`
		WHERE ($1::bigint = 0 OR n.id < $1)
		  AND ($2 = '' OR lower(n.title) LIKE '%' || lower($2) || '%')
		  AND ($3 = '' OR EXISTS (SELECT 1 FROM note_tags x WHERE x.note_id = n.id AND x.tag = $3))
		GROUP BY n.id
		ORDER BY n.id DESC
		LIMIT $4`, f.AfterID, f.Query, f.Tag, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Note, 0, f.Limit)
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Update — частичное обновление одним запросом; updated_at меняет триггер.
func (r NoteRepo) Update(ctx context.Context, id int64, u models.NoteUpdate) (models.Note, error) {
	tag, err := r.DB.Exec(ctx, `
		UPDATE notes SET
			title   = COALESCE($2, title),
			content = COALESCE($3, content)
		WHERE id = $1`, id, u.Title, u.Content)
	if err != nil {
		return models.Note{}, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return models.Note{}, models.ErrNotFound
	}
	return r.Get(ctx, id)
}

func (r NoteRepo) Delete(ctx context.Context, id int64) error {
	tag, err := r.DB.Exec(ctx, `DELETE FROM notes WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}
