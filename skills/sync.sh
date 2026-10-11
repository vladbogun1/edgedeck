#!/usr/bin/env bash
# Ставит или обновляет скиллы Claude Code из GitHub: все папки skills/*/ с SKILL.md.
#
#   bash skills/sync.sh            # из main
#   bash skills/sync.sh <ветка|тег>
#
# Источник — папка skills/ приватного репозитория vladbogun1/edgedeck, поэтому нужен
# залогиненный gh (gh auth status). Каждый скилл ставится в ~/.claude/skills/<имя>
# целиком, прежняя версия заменяется.
set -euo pipefail

REF="${1:-main}"
REPO="vladbogun1/edgedeck"
ROOT="$HOME/.claude/skills"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

gh repo clone "$REPO" "$TMP/repo" -- --depth 1 --branch "$REF" --filter=blob:none --sparse --quiet
git -C "$TMP/repo" sparse-checkout set skills
SHA="$(git -C "$TMP/repo" rev-parse --short HEAD)"
mkdir -p "$ROOT"
n=0
for d in "$TMP/repo/skills"/*/; do
  [ -f "$d/SKILL.md" ] || continue
  name="$(basename "$d")"
  rm -rf "$ROOT/$name"
  cp -R "$d" "$ROOT/$name"
  printf '%s@%s %s %s\n' "$REPO" "$REF" "$SHA" "$(date -u +%FT%TZ)" > "$ROOT/$name/.source"
  echo "скилл $name обновлён: $REPO@$REF ($SHA) -> $ROOT/$name"
  n=$((n+1))
done
[ "$n" -gt 0 ] || { echo "в $REPO@$REF нет skills/*/SKILL.md" >&2; exit 1; }
