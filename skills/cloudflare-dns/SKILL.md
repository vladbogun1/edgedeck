---
name: cloudflare-dns
description: DNS-записи доменов пользователя в Cloudflare (сейчас зона bogun.site) — посмотреть, добавить, изменить или удалить A/CNAME/TXT/MX-запись, завести поддомен под новый сайт или сервис на серверах s1/s2/s3, проверить, что зона и делегирование в порядке. Используй, когда нужно «добавь поддомен», «заведи A-запись», «направь домен на сервер», «проверь DNS», «перенеси домен в Cloudflare», или когда для нового сайта EdgeDeck нужна запись.
---

# Cloudflare DNS

Утилита — `scripts/cf.py` (только стандартная библиотека Python). Токен лежит в
`D:\CLOUDE\_private\cloudflare.env` (`CF_API_TOKEN`, `CF_ACCOUNT_ID`) — скрипт читает его сам.
**Токен никогда не выводить** (не `cat` этот файл, не печатать переменные).

```bash
python scripts/cf.py verify                 # токен жив, какие зоны видит
python scripts/cf.py zones                  # статус зоны (active/pending) и её NS
python scripts/cf.py list bogun.site        # все записи; «s1/s2/s3» — на какой наш сервер
python scripts/cf.py set bogun.site <имя> A <IP> --comment "s1: что это" [--dry-run]
python scripts/cf.py set bogun.site www CNAME bogun.site
python scripts/cf.py delete bogun.site <имя> A --yes
python scripts/cf.py check bogun.site       # оранжевое облако, чужие IP, что видит мир
```

На Windows Python — `C:/Users/nikto/AppData/Local/Programs/Python/Python313/python.exe`
(просто `python` в Git Bash может оказаться заглушкой Microsoft Store).

## Правила

- **Записи на свои серверы — только «серые» (`proxied: false`, по умолчанию в `set`).**
  EdgeDeck проверяет, что A-запись смотрит на сервер, и сам выпускает сертификаты; через
  прокси Cloudflare домен смотрит на Cloudflare — новые сертификаты не выпустятся, а сайты
  получат IP Cloudflare вместо клиента. `--proxied` — только если пользователь прямо попросил
  и понимает это.
- Серверы: **s1** `132.145.132.80` (прод), **s2** `150.136.127.73` (VPN/игры; `:443` занят
  xray — https-сайтов там нет), **s3** `150.136.40.217` (новое, демо, commerce-engine).
- Имя пиши коротко (`portainer`, `admin.engine`) или полностью (`portainer.bogun.site`) —
  скрипт достроит. Никогда не дописывай зону дважды: `x.bogun.site.bogun.site` — это ошибка
  (такие дубли уже были после импорта из Hostinger, их убрали 11.10.2026).
- Комментарий в записи — «сервер: что это» (`--comment "s3: магазин demo2"`): по нему потом
  видно, зачем запись.
- Перед удалением — `list`, убедиться, что запись не нужна (сайт снят с маршрутов EdgeDeck).
- Новая запись разойдётся по миру за минуты (TTL авто = 300 с), но резолверы, которые раньше
  получали «нет такого имени», могут помнить это до ~30 мин.

## Новый сайт целиком

1. `cf.py set bogun.site <имя> A <IP сервера> --comment "…"`.
2. Проверка: `nslookup <имя>.bogun.site 1.1.1.1` → IP сервера.
3. Маршрут в EdgeDeck — скилл `edgedeck` (`routes.yaml` → `--plan` → `--apply`).

## Состояние на 11.10.2026

Зона `bogun.site` в Cloudflare (`active`, NS `reza`/`sterling.ns.cloudflare.com`), перенесена
из Hostinger. Записи: корень и `www` (хаб EdgeDeck, s1), `s1`/`s2`/`s3` (панели), `jarvis`,
`bots`, `portainer`, `puffer` (s1), `pp-s2` (нода PufferPanel на s2), `ai`, `mr`, `test`,
`engine`, `admin.engine`, `app.engine` (s3). Почты (MX/TXT) у домена нет. Другие домены
(`maxsolch.com`, `chisetup.com.ua`, `tgdesk.tech`, `maxsolkh.shop`) — не в этом аккаунте.
Токену выданы и ключи R2 (там же, `R2_*`) — пока не используются.
