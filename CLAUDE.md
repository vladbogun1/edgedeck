# EdgeDeck — правила для Claude

- Скилл Claude Code для работы с EdgeDeck лежит в `skills/edgedeck/` (SKILL.md + references).
  **Любое изменение поведения** — поля `routes.yaml`, CLI (`--plan/--apply`), деплой, вход,
  серверы, грабли — обновляй скилл в том же коммите, затем после `git push` выполни
  `powershell -ExecutionPolicy Bypass -File skills/sync.ps1`: он поставит версию из GitHub
  в `~/.claude/skills/edgedeck`. Скилл должен совпадать с тем, что реально работает.
- Сборка только в CI; на серверы образ приносится артефактом (см. `skills/edgedeck/references/ops.md`).
- Генератор (`internal/routes/render.go`): для уже существующих маршрутов вывод менять нельзя
  без явной причины — сайты на серверах сравниваются с ним байт в байт.
- Go локально не нужен: `docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine go test ./...`.
- Предпросмотр интерфейса без сервера: `cd web && npx vite --config vite.mock.config.ts`.
