# EdgeDeck

Панель маршрутизации сервера: домены, порты, сертификаты и цели (контейнеры,
процессы вне докера, статика) в одном списке. Ставится поверх существующего
Caddy, работает в докере, управляет только тем сервером, где развёрнута.

**Дизайн и обоснования:** [DESIGN.md](DESIGN.md) — там же каталог грабель,
собранный на трёх серверах, из которого выросли почти все решения.

---

## Главная идея

Панель — **обработчик по умолчанию**. Любой домен, у которого ещё нет маршрута,
и заход напрямую по IP попадают в неё:

```
домен без маршрута  ─┐
заход по IP         ─┴─→  панель (сертификат выпускается на месте)
домен с маршрутом   ───→  сайт
```

Поэтому цикл добавления сайта короткий: направил A-запись → открыл домен →
попал в панель → домен уже в списке «пришли, но не настроены» → настроил.

Источник истины — файлы в `~/edge`, панель лишь удобный редактор поверх них.
Панель может умереть, сайты продолжат работать.

## Установка

Нужен сервер, где уже есть `~/edge` с Caddy (контейнер `edge_caddy`, сеть `edge`).

```bash
mkdir -p ~/edgedeck && cd ~/edgedeck
curl -fsSL -o install.sh https://raw.githubusercontent.com/vladbogun1/edgedeck/main/deploy/install.sh
curl -fsSL -o docker-compose.yml https://raw.githubusercontent.com/vladbogun1/edgedeck/main/deploy/docker-compose.yml
curl -fsSL -o _default.caddy https://raw.githubusercontent.com/vladbogun1/edgedeck/main/deploy/_default.caddy
bash install.sh
```

Скрипт создаст `.env` со случайными токенами и стартовым паролем, поправит
`Caddyfile` (с копией), поставит обработчик по умолчанию и поднимет панель.
Репозиторий и пакет приватные, поэтому образ нужно откуда-то взять. Два способа:

**Токен с `read:packages`** — обычный путь:

```bash
echo "$GITHUB_TOKEN_WITH_READ_PACKAGES" | docker login ghcr.io -u vladbogun1 --password-stdin
```

**Артефакт сборки** — если такого токена нет: тот же образ CI выкладывает файлом,
и он качается по обычным правам на репозиторий.

```bash
gh run download --repo vladbogun1/edgedeck -n edgedeck-arm64
scp edgedeck-arm64.tar ubuntu@СЕРВЕР:/tmp/
ssh ubuntu@СЕРВЕР 'docker load -i /tmp/edgedeck-arm64.tar && cd ~/edgedeck && docker compose up -d'
```

## Обновление

Сборки идут только в GitHub Actions, на сервер приходит готовый образ:

```bash
cd ~/edgedeck
sed -i 's|^EDGEDECK_IMAGE=.*|EDGEDECK_IMAGE=ghcr.io/vladbogun1/edgedeck:sha-XXXXXXX|' .env
docker compose pull && docker compose up -d
```

Тег `sha-<хеш>` — то, на что можно откатиться. `main` — последний коммит.

## Что внутри

| | |
|---|---|
| `cmd/edgedeck` | точка входа; `--agent` включает режим сайдкара |
| `internal/agent` | доступ к Docker: белый список из шести операций |
| `internal/server` | HTTP, API, вход, разрешение на сертификаты |
| `internal/dockerinfo` | контейнеры как цели: сети, порты, догадки о порте |
| `internal/hostports` | порты процессов вне докера (через `/proc` хоста) |
| `internal/caddyx` | admin API Caddy и чтение сертификатов из тома |
| `internal/sites` | разбор существующих `sites/*.caddy` |
| `internal/netcheck` | «домен смотрит сюда?» и предполётный стук в цель |
| `web` | интерфейс (React + Vite), вшивается в бинарник |

## Разработка

Go локально не нужен — проверка компиляции через докер:

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine go vet ./...
```

Интерфейс:

```bash
cd web && npm install && npm run dev    # /api уходит на панель, поднятую отдельно
```

## Права

Панель умеет ровно три вещи в системе: читать docker через агента, подключать
контейнеры к сети `edge` и писать в `~/edge`. Она **не** запускает контейнеры,
**не** правит чужие compose-файлы и **не** трогает фаервол — для фаервола она
показывает готовую команду и объясняет, какой слой в данном случае работает.
