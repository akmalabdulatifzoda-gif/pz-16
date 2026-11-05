-- 25 заметок с детерминированными заголовками и тегами для тестов списка.
-- id 1..25; каждая 5-я — с тегом "go", каждая 3-я — с тегом "db"; в заголовках 1..10 есть слово "Redis".
INSERT INTO notes (title, content)
SELECT CASE WHEN g <= 10 THEN 'Redis note ' || g ELSE 'Plain note ' || g END,
       'content ' || g
FROM generate_series(1, 25) AS g;

INSERT INTO note_tags (note_id, tag)
SELECT id, 'go' FROM notes WHERE id % 5 = 0
UNION ALL
SELECT id, 'db' FROM notes WHERE id % 3 = 0;
