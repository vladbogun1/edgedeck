# Эксплуатация EdgeDeck

## Серверы

Все — Oracle Cloud Always Free, ARM64, Ubuntu 22.04. SSH одним ключом:

```bash
ssh -i C:/Users/nikto/oracleserver.key ubuntu@132.145.132.80   # s1, прод
ssh -i C:/Users/nikto/oracleserver.key ubuntu@150.136.127.73   # s2, VPN/боты: только :80, :443 = xray
ssh -i C:/Users/nikto/oracleserver.key ubuntu@150.136.40.217   # s3 (oracle-3), новое и демо
```

Ключа нет в `~/.ssh` — всегда `-i`.

Адреса панелей: **`https://bogun.site`** — хаб (обзор флота, страница единого входа),
`https://s1.bogun.site`, `https://s3.bogun.site`, `http://s2.bogun.site` (s2 — только http,
пока `:443` у xray). JARVIS — `https://jarvis.bogun.site` (за единым входом),
Vaultwarden — `https://bogun.site/vault/` (свой вход), логи — `https://bogun.site/logs/`.
Portainer — `https://portainer.bogun.site` (все три сервера внутри), PufferPanel —
`https://puffer.bogun.site` — одна панель на все серверы (с 11.10.2026): главная на s1,
s2 — удалённая нода `S2`. Каналы: панель → нода `https://pp-s2.bogun.site:8443` (Caddy s2,
файл руками `sites/pp-s2.bogun.site.caddy`, только с IP s1; порт открыт в Oracle Security List
s2 для s1), нода → панель `https://pp-master.bogun.site` (маршрут s1, `allow_ips` = IP s2, без
единого входа). На s2 PufferPanel в режиме ноды (`panel.enable: false`), слушает
`172.17.0.1:8080` (мост Docker) — снаружи закрыт. Portainer на s1 опубликован только на
`127.0.0.1` (9443/8082/8000). SFTP ноды s2 (`:5657`) снаружи закрыт.
Оба за единым входом, свой вход у каждого остаётся. DNS `bogun.site` — в Cloudflare
(скилл `cloudflare-dns`). Полная карта сервисов: `D:\CLOUDE\_kb\servers.md`.

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

## Флот (все серверы в одной панели)

Раздел «Флот» и переключатель серверов в меню. Каждая панель отдаёт сводку о себе
(`/api/fleet/summary`) только по запросу, подписанному общим секретом; ответ тоже подписан,
поэтому соседей спрашивают по IP (s2 — по http, `:443` там занят xray).

В `~/edgedeck/.env` каждого сервера (и строки в `docker-compose.yml` панели):
- `EDGEDECK_FLEET_SECRET` — **одинаковый** на всех серверах (не выводить в чат);
- `EDGEDECK_FLEET` — реестр `id|подпись|адрес для панелей|адрес для браузера` через запятую,
  одинаковый на всех: `s1|прод|https://132.145.132.80|https://s1.bogun.site,s2|vpn и игры|http://150.136.127.73|http://s2.bogun.site,s3|новое|https://150.136.40.217|https://s3.bogun.site`;
- `EDGEDECK_SERVER_ID` — `s1` / `s2` / `s3`;
- `EDGEDECK_TRUSTED_ZONES=bogun.site` на s2 (там нет своих маршрутов в `bogun.site`).

Новый сервер: дописать его в `EDGEDECK_FLEET` **на всех** серверах, скопировать секрет с
любого (`grep ^EDGEDECK_FLEET_SECRET= ~/edgedeck/.env` по SSH, в файл, не в чат),
`docker compose up -d --no-deps panel` везде. Проверка — в разделе «Флот» сервер зелёный;
«разный секрет флота» в ошибке — секреты не совпали.

## Единый вход (`*.bogun.site`)

Вход в любую панель ставит cookie `edgedeck_sso` на `.bogun.site` (30 дней, подпись ключом
из секрета флота) — она пускает во все панели и во все маршруты с `sso: true` на всех
серверах. Страница входа — хаб `https://bogun.site` (`EDGEDECK_SSO_LOGIN`), домен —
`EDGEDECK_SSO_DOMAIN=bogun.site` в `.env` всех серверов. Caddy вырезает эту cookie из
запросов ко всем приложениям, кроме самой панели (`header_up Cookie …` в каждом
`reverse_proxy`), — приложения на поддоменах её не видят. «Настройки → Выйти везде»
отзывает токены на всех серверах флота.

Закрыть сервис входом: в маршруте `sso: true` → `--plan` → `--apply`. Сервис должен быть
на `*.bogun.site` и по https (cookie `Secure`) — поэтому s2 до деления `:443` с xray не годится.

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
