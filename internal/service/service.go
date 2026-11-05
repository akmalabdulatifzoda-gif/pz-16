// Package service — бизнес-логика заметок.
package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/akmalabdulatifzoda-gif/pz-16/internal/models"
)

// Notes — то, что сервису нужно от хранилища.
type Notes interface {
	Create(ctx context.Context, n *models.Note) error
	Get(ctx context.Context, id int64) (models.Note, error)
	List(ctx context.Context, f models.ListFilter) ([]models.Note, error)
	Update(ctx context.Context, id int64, u models.NoteUpdate) (models.Note, error)
	Delete(ctx context.Context, id int64) error
}

type Service struct{ Notes Notes }

const (
	MaxTitle     = 200
	DefaultLimit = 20
	MaxLimit     = 100
)

func validTitle(t string) error {
	t = strings.TrimSpace(t)
	if t == "" {
		return fmt.Errorf("%w: title is required", models.ErrValidation)
	}
	if len([]rune(t)) > MaxTitle {
		return fmt.Errorf("%w: title is longer than %d characters", models.ErrValidation, MaxTitle)
	}
	return nil
}

// Create валидирует заголовок и нормализует теги (нижний регистр, без пробелов).
// Формат тега дополнительно проверяет CHECK-ограничение в БД.
func (s Service) Create(ctx context.Context, in models.NoteCreate) (models.Note, error) {
	if err := validTitle(in.Title); err != nil {
		return models.Note{}, err
	}
	n := models.Note{Title: strings.TrimSpace(in.Title), Content: in.Content, Tags: make([]string, 0, len(in.Tags))}
	for _, t := range in.Tags {
		n.Tags = append(n.Tags, strings.ToLower(strings.TrimSpace(t)))
	}
	err := s.Notes.Create(ctx, &n)
	return n, err
}

func (s Service) Get(ctx context.Context, id int64) (models.Note, error) {
	return s.Notes.Get(ctx, id)
}

func (s Service) List(ctx context.Context, f models.ListFilter) (models.Page, error) {
	if f.Limit == 0 {
		f.Limit = DefaultLimit
	}
	if f.Limit < 1 || f.Limit > MaxLimit {
		return models.Page{}, fmt.Errorf("%w: limit must be 1..%d", models.ErrValidation, MaxLimit)
	}
	items, err := s.Notes.List(ctx, f)
	if err != nil {
		return models.Page{}, err
	}
	p := models.Page{Items: items}
	if len(items) == f.Limit {
		p.NextCursor = fmt.Sprint(items[len(items)-1].ID)
	}
	return p, nil
}

func (s Service) Update(ctx context.Context, id int64, u models.NoteUpdate) (models.Note, error) {
	if u.Title == nil && u.Content == nil {
		return models.Note{}, fmt.Errorf("%w: nothing to update", models.ErrValidation)
	}
	if u.Title != nil {
		if err := validTitle(*u.Title); err != nil {
			return models.Note{}, err
		}
		t := strings.TrimSpace(*u.Title)
		u.Title = &t
	}
	return s.Notes.Update(ctx, id, u)
}

func (s Service) Delete(ctx context.Context, id int64) error {
	return s.Notes.Delete(ctx, id)
}
