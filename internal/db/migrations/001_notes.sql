-- Заметки. Ограничения дублируют валидацию сервиса: БД — последняя линия защиты.
CREATE TABLE IF NOT EXISTS notes (
    id         BIGSERIAL   PRIMARY KEY,
    title      TEXT        NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    content    TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- updated_at выставляет сама БД при любом UPDATE.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = clock_timestamp();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notes_updated ON notes;
CREATE TRIGGER trg_notes_updated
    BEFORE UPDATE ON notes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Поиск по подстроке заголовка без учёта регистра.
CREATE INDEX IF NOT EXISTS idx_notes_title_lower ON notes (lower(title));
