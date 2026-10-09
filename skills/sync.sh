#!/usr/bin/env bash
# Ставит или обновляет скилл Claude Code «edgedeck» из GitHub.
#
#   bash skills/sync.sh            # из main
#   bash skills/sync.sh <ветка|тег>
#
# Источник — папка skills/edgedeck приватного репозитория vladbogun1/edgedeck,
# поэтому нужен залогиненный gh (gh auth status). Ставится в
# ~/.claude/skills/edgedeck целиком, прежняя версия заменяется.
set -euo pipefail

REF="${1:-main}"
REPO="vladbogun1/edgedeck"
DEST="$HOME/.claude/skills/edgedeck"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

gh repo clone "$REPO" "$TMP/repo" -- --depth 1 --branch "$REF" --filter=blob:none --sparse --quiet
git -C "$TMP/repo" sparse-checkout set skills/edgedeck
[ -f "$TMP/repo/skills/edgedeck/SKILL.md" ] || { echo "в $REPO@$REF нет skills/edgedeck/SKILL.md" >&2; exit 1; }
SHA="$(git -C "$TMP/repo" rev-parse --short HEAD)"

mkdir -p "$(dirname "$DEST")"
rm -rf "$DEST"
cp -R "$TMP/repo/skills/edgedeck" "$DEST"
printf '%s@%s %s %s\n' "$REPO" "$REF" "$SHA" "$(date -u +%FT%TZ)" > "$DEST/.source"
echo "скилл edgedeck обновлён: $REPO@$REF ($SHA) -> $DEST"
