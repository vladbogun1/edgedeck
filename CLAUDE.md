# EdgeDeck — правила для Claude

- План доработок — `PLAN.md` (P0/P1/P2, вопросы), где остановились — `HANDOFF.md`.
- Скиллы Claude Code лежат в `skills/`: `edgedeck` (панель, маршруты, серверы) и `cloudflare-dns`
  (DNS-записи в Cloudflare, утилита `scripts/cf.py`, токен — `D:\CLOUDE\_private\cloudflare.env`).
  **Любое изменение поведения** — поля `routes.yaml`, CLI (`--plan/--apply`), деплой, вход,
  серверы, грабли — обновляй скилл в том же коммите, затем после `git push` выполни
  `powershell -ExecutionPolicy Bypass -File skills/sync.ps1`: он поставит версию из GitHub
  в `~/.claude/skills/<имя>` (все скиллы из `skills/`). Скилл должен совпадать с тем, что реально работает.
- Сборка только в CI; на серверы образ приносится артефактом (см. `skills/edgedeck/references/ops.md`).
- Генератор (`internal/routes/render.go`): для уже существующих маршрутов вывод менять нельзя
  без явной причины — сайты на серверах сравниваются с ним байт в байт.
- Go локально не нужен: `docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine go test ./...`.
- Предпросмотр интерфейса без сервера: `cd web && npx vite --config vite.mock.config.ts`.
