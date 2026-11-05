// Package httpapi — HTTP-слой: маршруты, разбор запросов, JSON-ответы.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/akmalabdulatifzoda-gif/pz-16/internal/models"
	"github.com/akmalabdulatifzoda-gif/pz-16/internal/service"
)

type Router struct {
	Svc  *service.Service
	Ping func(context.Context) error // проверка БД для /healthz
	Log  *slog.Logger
}

func (rt Router) Handler() http.Handler {
	if rt.Log == nil {
		rt.Log = slog.Default()
	}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/healthz", rt.health)
	r.Route("/notes", func(r chi.Router) {
		r.Get("/", rt.list)
		r.Post("/", rt.create)
		r.Get("/{id}", rt.get)
		r.Patch("/{id}", rt.update)
		r.Delete("/{id}", rt.delete)
	})
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail переводит доменные ошибки в HTTP-коды и логирует неожиданные (в т.ч. SQL-ошибки).
func (rt Router) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, models.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, models.ErrValidation):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		rt.Log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

// decode читает JSON строго: неизвестные поля и мусор после объекта — 400.
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected data after JSON object")
	}
	return nil
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id > 0
}

func (rt Router) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if rt.Ping != nil {
		if err := rt.Ping(ctx); err != nil {
			writeErr(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (rt Router) create(w http.ResponseWriter, r *http.Request) {
	var in models.NoteCreate
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	n, err := rt.Svc.Create(r.Context(), in)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/notes/"+strconv.FormatInt(n.ID, 10))
	writeJSON(w, http.StatusCreated, n)
}

func (rt Router) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	n, err := rt.Svc.Get(r.Context(), id)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (rt Router) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := models.ListFilter{Query: q.Get("q"), Tag: q.Get("tag")}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if n == 0 {
			n = -1 // limit=0 явно передан — это ошибка, а не «по умолчанию»
		}
		f.Limit = n
	}
	if v := q.Get("cursor"); v != "" {
		c, err := strconv.ParseInt(v, 10, 64)
		if err != nil || c <= 0 {
			writeErr(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		f.AfterID = c
	}
	p, err := rt.Svc.List(r.Context(), f)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (rt Router) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var u models.NoteUpdate
	if err := decode(r, &u); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	n, err := rt.Svc.Update(r.Context(), id, u)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (rt Router) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := rt.Svc.Delete(r.Context(), id); err != nil {
		rt.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
