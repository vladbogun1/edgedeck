#!/usr/bin/env bash
# Установка EdgeDeck на сервер, где уже есть ~/edge с Caddy.
#
# Что делает:
#   1. создаёт ~/edgedeck (compose + .env + data), если ещё нет;
#   2. правит ~/edge/Caddyfile: admin по сети + блок on_demand_tls;
#   3. ставит ~/edge/sites/_default.caddy — обработчик по умолчанию → панель;
#   4. тянет образ и поднимает панель;
#   5. проверяет конфиг Caddy и применяет его без разрыва соединений.
#
# Всё идемпотентно: повторный запуск ничего не ломает. Перед правкой Caddyfile
# делается копия с датой.
set -euo pipefail

IMAGE="${EDGEDECK_IMAGE:-ghcr.io/vladbogun1/edgedeck:main}"
EDGE_DIR="${EDGE_DIR:-$HOME/edge}"
DECK_DIR="${DECK_DIR:-$HOME/edgedeck}"
EDGE_NETWORK="${EDGEDECK_EDGE_NETWORK:-edge}"
CADDY_CONTAINER="${CADDY_CONTAINER:-edge_caddy}"

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }

[ -d "$EDGE_DIR" ] || { echo "нет каталога $EDGE_DIR — сначала должен быть развёрнут Caddy"; exit 1; }
[ -f "$EDGE_DIR/Caddyfile" ] || { echo "нет $EDGE_DIR/Caddyfile"; exit 1; }

say "1. Каталог панели: $DECK_DIR"
mkdir -p "$DECK_DIR/data"
SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ "$SRC_DIR" != "$DECK_DIR" ]; then
  cp -f "$SRC_DIR/docker-compose.yml" "$DECK_DIR/docker-compose.yml"
  note "docker-compose.yml обновлён"
else
  note "скрипт запущен из каталога панели — файлы уже на месте"
fi

if [ ! -f "$DECK_DIR/.env" ]; then
  SERVER_IP="$(curl -4 -s --max-time 5 https://api.ipify.org || true)"
  PASSWORD="$(head -c 12 /dev/urandom | base64 | tr -d '+/=' | head -c 14)"
  cat > "$DECK_DIR/.env" <<EOF
# Образ. Обновление панели = смена тега здесь + docker compose up -d.
EDGEDECK_IMAGE=$IMAGE

# Каталоги и сети
EDGE_DIR=$EDGE_DIR
EDGEDECK_EDGE_NETWORK=$EDGE_NETWORK
CADDY_DATA_VOLUME=${CADDY_DATA_VOLUME:-edge_caddy_data}
EDGEDECK_CADDY_ADMIN=http://$CADDY_CONTAINER:2019

# Внешний адрес: по нему панель проверяет, что домен смотрит именно сюда.
EDGEDECK_PUBLIC_IP=$SERVER_IP

# Секрет между панелью и агентом: в сети edge живут и чужие контейнеры.
EDGEDECK_AGENT_TOKEN=$(head -c 24 /dev/urandom | base64 | tr -d '+/=')

# Секрет в пути /internal/tls-ask/<token>: этот путь зовёт только Caddy.
EDGEDECK_TLS_ASK_TOKEN=$(head -c 24 /dev/urandom | base64 | tr -d '+/=')

# Стартовый пароль. После первого входа смените его в панели и можете убрать.
EDGEDECK_INITIAL_PASSWORD=$PASSWORD
EOF
  chmod 600 "$DECK_DIR/.env"
  note ".env создан, стартовый пароль: $PASSWORD"
else
  note ".env уже есть — не трогаю (пароль и токены сохранены)"
  sed -i "s|^EDGEDECK_IMAGE=.*|EDGEDECK_IMAGE=$IMAGE|" "$DECK_DIR/.env"
  note "образ в .env обновлён на $IMAGE"
fi

# shellcheck disable=SC1091
set -a; . "$DECK_DIR/.env"; set +a

say "2. Правка $EDGE_DIR/Caddyfile"
STAMP="$(date +%Y%m%d-%H%M%S)"
cp -f "$EDGE_DIR/Caddyfile" "$EDGE_DIR/Caddyfile.bak-$STAMP"
note "копия: Caddyfile.bak-$STAMP"

# ВАЖНО. Caddyfile смонтирован в контейнер как ОДИН ФАЙЛ. Docker привязывает
# такой монтаж к иноду, поэтому любая правка «через временный файл и mv»
# (а `sed -i` работает именно так) отвязывает контейнер от файла: хост видит
# новую версию, контейнер — навсегда старую, и ошибка получается загадочная.
# Поэтому правим только содержимое, сохраняя инод: python открывает файл на
# запись и усекает его на месте.
python3 - "$EDGE_DIR/Caddyfile" "$EDGEDECK_TLS_ASK_TOKEN" "${EDGEDECK_PUBLIC_IP:-}" <<'PY'
import re, sys

path, token = sys.argv[1], sys.argv[2]
server_ip = sys.argv[3] if len(sys.argv) > 3 else ''
src = open(path, encoding='utf-8').read()
changed = []

# admin должен слушать сеть edge, иначе панель не сможет применять конфиг.
new = re.sub(r'(?m)^(\s*)admin\s+localhost:2019\s*$', r'\1admin :2019', src)
if new != src:
    src = new
    changed.append('admin: localhost:2019 -> :2019 (порт наружу не публикуется)')

# Разрешение на сертификат для незнакомого домена спрашивается у панели.
if 'on_demand_tls' not in src:
    block = (
        "\n\t# Сертификаты для доменов, которых нет в sites/: выпускаются на первый\n"
        "\t# запрос, но только если панель подтвердит, что домен смотрит на этот\n"
        "\t# сервер. Без этой проверки чужой домен сжёг бы наши лимиты Let's Encrypt.\n"
        "\t# (interval/burst тут больше не поддерживаются — ограничение делает панель.)\n"
        "\ton_demand_tls {\n"
        f"\t\task http://edgedeck:4000/internal/tls-ask/{token}\n"
        "\t}\n"
    )
    m = re.search(r'^\{\s*$', src, re.M)
    if m:
        src = src[:m.end()] + block + src[m.end():]
    else:
        src = "{" + block + "}\n\n" + src
    changed.append('добавлен блок on_demand_tls')

# Заход по IP. Браузеры и curl не отправляют SNI для адресов, а соединение без
# SNI иначе попадает в catch-all с on-demand и подвисает. default_sni добавляет
# пустой SNI в сопоставление политики и подставляет имя при выборе сертификата.
#
# Важно: работает это только В ПАРЕ с явным файлом сертификата для IP (его
# делает следующий шаг установщика). С `tls internal` та же связка уводила имя
# в ACME, а Let's Encrypt для IP-адресов сертификаты не выдаёт вообще
# («Default profile does not permit IP address identifiers») — рукопожатие висло.
if server_ip and 'default_sni' not in src:
    m = re.search(r'^\{\s*$', src, re.M)
    line = (
        "\n\t# Заход по IP: браузеры не отправляют SNI для адресов, поэтому\n"
        "\t# соединение без SNI считаем обращением к самому серверу.\n"
        f"\tdefault_sni {server_ip}\n"
    )
    if m:
        src = src[:m.end()] + line + src[m.end():]
    else:
        src = "{" + line + "}\n\n" + src
    changed.append(f'добавлен default_sni {server_ip}')

if changed:
    with open(path, 'w', encoding='utf-8') as f:   # усечение на месте, инод тот же
        f.write(src)
    for c in changed:
        print('  ' + c)
else:
    print('  Caddyfile уже настроен')
PY

# Проверка на случай, если инод всё-таки отвязался раньше (например, файл когда-то
# правили через sed -i): контейнер тогда живёт со старым конфигом.
host_sum="$(sha256sum "$EDGE_DIR/Caddyfile" | cut -d' ' -f1)"
cont_sum="$(docker exec "$CADDY_CONTAINER" sha256sum /etc/caddy/Caddyfile 2>/dev/null | cut -d' ' -f1 || true)"
if [ -n "$cont_sum" ] && [ "$host_sum" != "$cont_sum" ]; then
  note "контейнер видит другой Caddyfile — пересоздаю $CADDY_CONTAINER, чтобы монтаж привязался заново"
  (cd "$EDGE_DIR" && docker compose up -d --force-recreate caddy >/dev/null)
  sleep 2
fi

say "3. Сертификат для захода по IP"
# Let's Encrypt для IP-адресов сертификаты не выдаёт, а внутренний CA Caddy в
# паре с on-demand на том же порту ведёт себя непредсказуемо. Поэтому — свой
# самоподписанный сертификат: он не истекает через 90 дней, ничего не выпускает
# по сети и не тратит лимиты. Браузер предупредит один раз, это ожидаемо.
mkdir -p "$EDGE_DIR/certs"
IP_FOR_CERT="${EDGEDECK_PUBLIC_IP:-127.0.0.1}"
if [ ! -f "$EDGE_DIR/certs/ip.crt" ] || ! openssl x509 -noout -checkend 2592000 -in "$EDGE_DIR/certs/ip.crt" 2>/dev/null; then
  openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
    -keyout "$EDGE_DIR/certs/ip.key" -out "$EDGE_DIR/certs/ip.crt" \
    -subj "/CN=$IP_FOR_CERT" -addext "subjectAltName=IP:$IP_FOR_CERT" >/dev/null 2>&1
  chmod 600 "$EDGE_DIR/certs/ip.key"
  note "сертификат для $IP_FOR_CERT создан (на 10 лет, самоподписанный)"
else
  note "сертификат для захода по IP уже есть"
fi

# Каталог с сертификатом нужно смонтировать в Caddy. Это единственная правка
# compose-файла edge, и она делается один раз.
if ! grep -q './certs:/etc/caddy/certs' "$EDGE_DIR/docker-compose.yml"; then
  cp -f "$EDGE_DIR/docker-compose.yml" "$EDGE_DIR/docker-compose.yml.bak-$STAMP"
  python3 - "$EDGE_DIR/docker-compose.yml" <<'PY'
import re, sys
path = sys.argv[1]
src = open(path, encoding='utf-8').read()
anchor = re.search(r'(?m)^(\s*)- \./sites:/etc/caddy/sites:ro\s*$', src)
if anchor:
    indent = anchor.group(1)
    line = f"\n{indent}# Самоподписанный сертификат для захода по IP (ставит EdgeDeck)\n{indent}- ./certs:/etc/caddy/certs:ro"
    src = src[:anchor.end()] + line + src[anchor.end():]
    open(path, 'w', encoding='utf-8').write(src)
    print('  монтаж ./certs добавлен в docker-compose.yml')
else:
    print('  ВНИМАНИЕ: не нашёл, куда добавить монтаж ./certs — добавьте вручную:')
    print('           - ./certs:/etc/caddy/certs:ro')
PY
  (cd "$EDGE_DIR" && docker compose up -d --force-recreate caddy >/dev/null 2>&1)
  note "контейнер Caddy пересоздан, чтобы монтаж применился"
else
  note "монтаж ./certs уже прописан"
fi

say "4. Обработчик по умолчанию"
if [ -f "$EDGE_DIR/sites/_default.caddy" ] && ! grep -q 'edgedeck:4000' "$EDGE_DIR/sites/_default.caddy"; then
  mv "$EDGE_DIR/sites/_default.caddy" "$EDGE_DIR/sites/_default.caddy.bak-$STAMP"
  note "старый _default.caddy сохранён как _default.caddy.bak-$STAMP"
fi
sed "s|__SERVER_IP__|${EDGEDECK_PUBLIC_IP:-127.0.0.1}|g" "$SRC_DIR/_default.caddy" \
  > "$EDGE_DIR/sites/_default.caddy"
note "_default.caddy → панель (домены с собственным файлом не затронуты)"

say "5. Запуск панели"
cd "$DECK_DIR"
# Пакет в GHCR приватный: если токена с read:packages нет, образ уже загружен
# локально из артефакта сборки (docker load). Тогда pull не должен ломать установку.
docker compose pull 2>/dev/null || note "из реестра не тянется — использую локальный образ"
docker compose up -d
docker compose ps --format '  {{.Name}}  {{.Status}}'

say "6. Проверка и применение конфига Caddy"
if docker exec "$CADDY_CONTAINER" caddy validate --config /etc/caddy/Caddyfile >/dev/null 2>&1; then
  docker exec "$CADDY_CONTAINER" caddy reload --config /etc/caddy/Caddyfile
  note "конфиг применён без перезапуска — соединения не рвались"
else
  echo "  ✗ конфиг не прошёл проверку. Вот что говорит Caddy:"
  docker exec "$CADDY_CONTAINER" caddy validate --config /etc/caddy/Caddyfile || true
  echo "  Ничего не применено. Откат: cp $EDGE_DIR/Caddyfile.bak-$STAMP $EDGE_DIR/Caddyfile"
  exit 1
fi

say "Готово"
note "Панель: http://${EDGEDECK_PUBLIC_IP:-<IP сервера>}/  (и на любом домене без маршрута)"
note "Логи:   cd $DECK_DIR && docker compose logs -f panel"
