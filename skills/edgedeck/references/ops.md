# Эксплуатация EdgeDeck

## Серверы

Все — Oracle Cloud Always Free, ARM64, Ubuntu 22.04. SSH одним ключом:

```bash
ssh -i C:/Users/nikto/oracleserver.key ubuntu@132.145.132.80   # s1, прод
ssh -i C:/Users/nikto/oracleserver.key ubuntu@150.136.127.73   # s2, VPN/боты: только :80, :443 = xray
ssh -i C:/Users/nikto/oracleserver.key ubuntu@150.136.40.217   # s3 (oracle-3), новое и демо
```

Ключа нет в `~/.ssh` — всегда `-i`. Полная карта сервисов: `D:\CLOUDE\_kb\servers.md`.

На каждом сервере:
- `~/edge` — Caddy (`edge_caddy`, сеть `edge`, том `edge_caddy_data` — **не удалять**:
  лимиты Let's Encrypt), `routes.yaml`, `sites/`, `snippets/`, `www/`.
- `~/edgedeck` — `docker-compose.yml`, `.env` (секреты), `data/` (пароль, сессии, журнал,
  снимки, `trusted-zones.json`), `state/` (снимок фаервола), `backup/`.
- Контейнеры: `edgedeck` (панель), `edgedeck-agent` (Docker, белый список),
  `edgedeck-firewall` (только читает iptables).

## CLI на сервере

```bash
docker exec edgedeck edgedeck --plan                  # неприменённые правки + что изменится + проверка Caddy, без записи
docker exec edgedeck edgedeck --apply --reason "…" --actor agent:имя   # снимок → запись → reload → контроль → откат
docker exec edgedeck edgedeck --apply --adopt a.caddy,b.caddy   # разрешить заменить файлы руками
docker exec edgedeck edgedeck --discard               # вернуть routes.yaml к последнему применённому
```

Применения и откаты из CLI пишутся в «Историю» и уходят в Telegram (`--actor` — кто,
по умолчанию `ssh`). Последний применённый `routes.yaml` — `~/edgedeck/data/applied/`
(только root: `sudo`); откат возвращает его, отвергнутая правка — в
`~/edgedeck/data/rejected/`. Если файл разошёлся с работающим, в панели на «Доменах»
висит жёлтая плашка с кнопками «Применить» / «Вернуть как было».

Откат к снимку — в панели «История» → «Вернуть»; снимки лежат в
`~/edgedeck/data/snapshots` обычными файлами.

## Обновить панель

Сборка только в GitHub Actions. На серверах нет `docker login ghcr.io`, поэтому образ
приносим артефактом:

```bash
gh run list --repo vladbogun1/edgedeck -L 3          # дождаться success
gh run download <run-id> --repo vladbogun1/edgedeck -n edgedeck-arm64
scp -i C:/Users/nikto/oracleserver.key edgedeck-arm64.tar ubuntu@<IP>:/tmp/
ssh … 'cd ~/edgedeck && docker tag ghcr.io/vladbogun1/edgedeck:main edgedeck:rollback-$(date +%Y%m%d) \
  && docker load -q -i /tmp/edgedeck-arm64.tar && rm /tmp/edgedeck-arm64.tar \
  && docker compose up -d --no-deps panel'
```

Проверка: `docker logs --since 1m edgedeck | grep version=` — там `sha-<коммит>`.
Откат: `docker tag edgedeck:rollback-YYYYMMDD ghcr.io/vladbogun1/edgedeck:main && docker compose up -d --no-deps panel`.

## Новый сервер

1. Сначала `~/edge` с Caddy: шаблон — `D:\CLOUDE\infra\oracle\server-3\edge`
   (Caddyfile, `bin/`, `snippets/`, compose), сеть `docker network create edge`.
2. EdgeDeck: файлы из `deploy/` репозитория (`install.sh`, `docker-compose.yml`,
   `_default.caddy`) → `bash install.sh`. Он создаст `.env` со случайными токенами,
   поправит Caddyfile (admin, on_demand_tls), поставит `_default.caddy` и поднимет панель.
3. Образ — артефактом, как в «Обновить панель».
4. В `.env` вписать `EDGEDECK_TELEGRAM_TOKEN` и `EDGEDECK_TELEGRAM_CHAT` (те же, что на
   других серверах, — взять из их `.env`, не выводя в чат) → `docker compose up -d`.
5. Добавить сервер в `D:\CLOUDE\_kb\servers.md` и в этот файл.

## Вход в панель

- Панель открывается по IP сервера и на любом поддомене своих зон без маршрута.
- Вход — кодом в личный Telegram (бот из `.env`): 5 минут, 5 попыток. Пароль — запасной.
- Забыт пароль: войти кодом → «Настройки» → «Пароль»; в первые 15 минут после входа
  текущий пароль не спрашивается.

## Чужие домены

Домен считается своим, если его зона (eTLD+1) есть среди сайтов, в
`EDGEDECK_TRUSTED_ZONES` или в `~/edgedeck/data/trusted-zones.json` (кнопка «это мой
домен»). Чужим — 421 без тела и отказ в сертификате; в Telegram одно сообщение на зону
в 30 дней. Так закрыт фишинг `*.amzcomverif.kozow.com`, смотревший на s1.

## Грабли

- **502** — чаще всего цель не в сети Caddy. Экран «Цели» показывает «не в сети edge» и
  кнопку подключения. `docker compose up --force-recreate` отбирает сеть `edge` — панель
  вернёт её за ~10 с.
- Docker-порты не закрываются ufw (идут через FORWARD) — только правилом в `DOCKER-USER`
  (`/opt/docker-acl/ports.conf` + `apply.sh`). Панель фаервол только читает.
- `127.0.0.1:PORT` недоступен из другого контейнера — ходить по docker-сети.
- `~/edge/bin/reload` съедает stdin — в `ssh 'bash -s' <<EOF` вызывать с `</dev/null`.
- Git Bash портит аргументы, похожие на пути (`/logs` → `C:/Program Files/Git/logs`):
  `MSYS_NO_PATHCONV=1` перед командой.
- Магазин держит свои порты :666/:667 на сертификате certbot для `maxsolkh.shop` — у этого
  маршрута `acme_webroot: /srv/certbot-webroot`, не убирать, пока порты нужны.
