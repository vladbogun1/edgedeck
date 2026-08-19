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
cp -f "$(dirname "$0")/docker-compose.yml" "$DECK_DIR/docker-compose.yml"
note "docker-compose.yml обновлён"

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

# admin должен слушать сеть edge, иначе панель не сможет применять конфиг.
if grep -q '^\s*admin\s\+localhost:2019' "$EDGE_DIR/Caddyfile"; then
  sed -i 's|^\(\s*\)admin\s\+localhost:2019|\1admin :2019|' "$EDGE_DIR/Caddyfile"
  note "admin: localhost:2019 → :2019 (порт наружу не публикуется)"
else
  note "admin уже настроен"
fi

# on_demand_tls: разрешение на сертификат для незнакомого домена спрашивается у панели.
if grep -q 'on_demand_tls' "$EDGE_DIR/Caddyfile"; then
  note "блок on_demand_tls уже есть"
else
  python3 - "$EDGE_DIR/Caddyfile" "$EDGEDECK_TLS_ASK_TOKEN" <<'PY'
import re, sys
path, token = sys.argv[1], sys.argv[2]
src = open(path, encoding='utf-8').read()
block = (
    "\n\t# Сертификаты для доменов, которых нет в sites/: выпускаются на первый\n"
    "\t# запрос, но только если панель подтвердит, что домен смотрит на этот\n"
    "\t# сервер. Без этой проверки чужой домен сжёг бы наши лимиты Let's Encrypt.\n"
    "\ton_demand_tls {\n"
    f"\t\task http://edgedeck:4000/internal/tls-ask/{token}\n"
    "\t\t# interval/burst у on_demand_tls в Caddy 2.11 уже не поддерживаются:\n"
    "\t\t# ограничение выпуска делает сама панель.\n"
    "\t}\n"
)
m = re.search(r'^\{\s*$', src, re.M)
if not m:
    src = "{" + block + "}\n\n" + src
else:
    src = src[:m.end()] + block + src[m.end():]
open(path, 'w', encoding='utf-8').write(src)
print("  блок on_demand_tls добавлен")
PY
fi

say "3. Обработчик по умолчанию"
if [ -f "$EDGE_DIR/sites/_default.caddy" ] && ! grep -q 'edgedeck:4000' "$EDGE_DIR/sites/_default.caddy"; then
  mv "$EDGE_DIR/sites/_default.caddy" "$EDGE_DIR/sites/_default.caddy.bak-$STAMP"
  note "старый _default.caddy сохранён как _default.caddy.bak-$STAMP"
fi
sed "s|__SERVER_IP__|${EDGEDECK_PUBLIC_IP:-127.0.0.1}|g" "$(dirname "$0")/_default.caddy" \
  > "$EDGE_DIR/sites/_default.caddy"
note "_default.caddy → панель (домены с собственным файлом не затронуты)"

say "4. Запуск панели"
cd "$DECK_DIR"
docker compose pull
docker compose up -d
docker compose ps --format '  {{.Name}}  {{.Status}}'

say "5. Проверка и применение конфига Caddy"
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
