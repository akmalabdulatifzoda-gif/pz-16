package models

import (
	"errors"
	"time"
)

// Note — заметка с тегами.
type Note struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NoteCreate — тело POST /notes.
type NoteCreate struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

// NoteUpdate — тело PATCH /notes/{id}; nil — поле не меняется.
type NoteUpdate struct {
	Title   *string `json:"title"`
	Content *string `json:"content"`
}

// Page — страница списка с курсором следующей страницы.
type Page struct {
	Items      []Note `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ListFilter — параметры GET /notes.
type ListFilter struct {
	Limit   int
	AfterID int64  // keyset: вернуть заметки с id < AfterID
	Query   string // подстрока заголовка, без учёта регистра
	Tag     string // точное совпадение тега
}

var (
	ErrNotFound   = errors.New("not found")
	ErrValidation = errors.New("validation error")
)
