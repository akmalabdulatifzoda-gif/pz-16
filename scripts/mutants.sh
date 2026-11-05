#!/usr/bin/env bash
# Мутационная проверка интеграционных тестов: по очереди «ломаем» схему или репозиторий
# в КОПИИ проекта и смотрим, какие тесты это заметят.
set -uo pipefail
cd "$(dirname "$0")/.."
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
mutant() {
  rm -rf "$TMP/p"; cp -r . "$TMP/p"
  python3 - "$TMP/p/$2" "${@:3}" <<'PY'
import sys
p, pairs = sys.argv[1], sys.argv[2:]
s = open(p).read()
for a, b in zip(pairs[::2], pairs[1::2]):
    assert a in s, a
    s = s.replace(a, b, 1)
open(p, "w").write(s)
PY
  echo "# Мутант: $1"
  (cd "$TMP/p" && go test -count=1 ./integration/... 2>&1 | grep -E -- "^--- FAIL|^(ok|FAIL)\s")
  echo
}
mutant "нет триггера updated_at"      internal/db/migrations/001_notes.sql 'FOR EACH ROW EXECUTE FUNCTION set_updated_at();' 'FOR EACH STATEMENT EXECUTE FUNCTION set_updated_at();'
mutant "нет ON DELETE CASCADE"        internal/db/migrations/002_tags.sql  'ON DELETE CASCADE' ''
mutant "заметка коммитится до вставки тегов (нет общей транзакции)" internal/repo/postgres.go \
  'if len(n.Tags) > 0 {' 'if err := tx.Commit(ctx); err != nil { return err }
	if len(n.Tags) > 0 {' \
  'if _, err := tx.Exec(ctx,
			`INSERT INTO note_tags' 'if _, err := r.DB.Exec(ctx,
			`INSERT INTO note_tags' \
  'if err := tx.Commit(ctx); err != nil {
		return err
	}
	tags, err' 'tags, err'
mutant "Delete не возвращает 404 для несуществующей" internal/repo/postgres.go 'if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil' '_ = tag
	return nil'
mutant "курсор включает последнюю запись (<= вместо <)" internal/repo/postgres.go 'OR n.id < $1)' 'OR n.id <= $1)'
