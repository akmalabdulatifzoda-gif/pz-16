-- Теги заметки: внешний ключ с каскадным удалением,
-- первичный ключ (note_id, tag) не даёт повторить тег у одной заметки.
CREATE TABLE IF NOT EXISTS note_tags (
    note_id BIGINT NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    tag     TEXT   NOT NULL CHECK (tag ~ '^[a-z0-9_-]{1,30}$'),
    PRIMARY KEY (note_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_note_tags_tag ON note_tags (tag);
