# routes.yaml — справочник

Файл `~/edge/routes.yaml`. Один маршрут = домен + путь. Все маршруты одного домена
собираются в один файл `sites/<домен>.caddy`. Настройки уровня сайта (заголовки,
сжатие, редиректы путей, HSTS, common, webroot) берутся из корневого маршрута (`path: /`).

## Минимальные примеры

```yaml
version: 1
routes:
    # контейнер
    - domain: app.example.com
      path: /
      type: proxy
      target: {kind: container, name: my-app, port: 3000}
      security_headers: true      # import common: nosniff, Referrer-Policy, X-Frame-Options, сжатие
      hsts: false
      comment: Что это и почему так.

    # статика из ~/edge/www/<домен>/ (SPA: try_files на /index.html)
    - domain: example.com
      aliases: [www.example.com]
      path: /
      type: static
      security_headers: true
      hsts: false

    # 301 с сохранением пути и query
    - domain: old.example.com
      path: /
      type: redirect
      redirect_to: https://example.com
      security_headers: false
      hsts: false
```

Типы: `proxy`, `static`, `redirect`, `acme_only` (только продление сертификата, остальное
404), `stub` (заглушка «скоро тут будет сайт»).

Виды цели: `container` (имя контейнера в сети Caddy), `host` (процесс вне докера →
`host.docker.internal:<порт>`; на s3 мешает REJECT в INPUT), `external` (`url: https://…`).

## Поля

| Поле | Уровень | Что делает |
|---|---|---|
| `aliases` | сайт | доп. имена в том же блоке (обычно `www`) |
| `path` | маршрут | `/` — весь сайт; `/logs/` → `handle /logs/*`; `/admin` → `handle /admin*`. Префикс **не** срезается |
| `security_headers` | сайт | `import common`. **Не ставить** для Telegram Mini App: его `X-Frame-Options SAMEORIGIN` ломает iframe Telegram Web — вместо этого `compress` + `headers` |
| `hsts` | сайт | `import hsts` (1 год). Билет в одну сторону: включать, только когда домен устоялся |
| `headers` | сайт | свои заголовки точно как нужно: `- {name: Strict-Transport-Security, value: "max-age=63072000; includeSubDomains"}`. `value: "-"` удаляет заголовок |
| `compress` | сайт | `encode zstd gzip` без остального common |
| `path_redirects` | сайт | `- {from: /logs, to: /logs/, code: 301}` (code по умолчанию 301) |
| `acme_webroot` | сайт | отдельный http-блок: `/.well-known/acme-challenge/*` из этой папки (в контейнере Caddy), остальное → https или туда же, куда редирект. Нужен, если сертификат на это имя выпускает ещё и чужой certbot |
| `max_upload` | сайт / путь | `128MB`. У корня — на весь сайт, у пути — только в его handle |
| `deny_paths` | сайт | 404 без тела. `/api/metrics` — ровно этот путь; `/admin/` или `/admin*` — весь раздел. Бьёт и по браузеру |
| `allow_ips` | сайт | пускать только с адресов/CIDR, остальным 403 |
| `sso` | путь | `true` — пускать только после единого входа EdgeDeck (код в Telegram): без входа редирект на `https://bogun.site/?rd=…` и обратно. Для своих сервисов (JARVIS, логи, Portainer, PufferPanel). **Не** для Vaultwarden и всего, у чего свои клиенты/API-токены |
| `auth` | путь | forward_auth: `{upstream: jarvis:4000, uri: /api/authcheck, header_up: [{name: X-Original-URI, value: "{uri}"}], copy_headers: [Cookie]}` |
| `hide_upstream_csp` + `csp` | маршрут | снять CSP апстрима и поставить свой. Свой CSP можно задать и через `headers` |
| `flush` | маршрут | `flush_interval -1`: события, стримы, камера (nginx `proxy_buffering off`) |
| `read_timeout`, `dial_timeout` | маршрут | `30s`, `5s`. По умолчанию таймаута чтения нет — WebSocket не рвутся |
| `comment` | маршрут | пояснение; попадает в шапку сгенерированного файла |

Всегда добавляется само: срез `Server`/`X-Powered-By`/`Via`, `header_up X-Real-IP`,
страница ошибки вместо голого 502 (с настоящим кодом), журнал
`/var/log/caddy/<домен>.log`.

## Пример со всем сразу (bogun.site на s1)

```yaml
    - domain: bogun.site
      aliases: [www.bogun.site]
      path: /
      type: proxy
      target: {kind: container, name: jarvis, port: 4000}
      security_headers: false
      hsts: false
      headers:
        - {name: Strict-Transport-Security, value: max-age=31536000}
        - {name: Referrer-Policy, value: same-origin}
      path_redirects:
        - {from: /logs, to: /logs/}
    - domain: bogun.site
      path: /logs/
      type: proxy
      target: {kind: container, name: dozzle, port: 8080}
      auth: {upstream: "jarvis:4000", uri: /api/authcheck, header_up: [{name: X-Original-URI, value: "{uri}"}]}
    - domain: bogun.site
      path: /vault/
      type: proxy
      target: {kind: container, name: vaultwarden, port: 80}
      max_upload: 128MB
```

## Чего модель пока не умеет

Если нужного нет в таблице — не писать `sites/*.caddy` руками молча. Варианты: добавить
поле в `internal/routes/model.go` + `render.go` (с тестом в `render_test.go` и проверкой,
что вывод для существующих маршрутов не изменился), либо оставить файл руками —
панель его не тронет и покажет в «Написаны руками».
