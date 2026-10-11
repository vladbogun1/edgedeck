#!/usr/bin/env python3
"""DNS-записи в Cloudflare — для скилла cloudflare-dns. Только стандартная библиотека.

    cf.py verify                         токен жив? что ему разрешено
    cf.py zones                          зоны аккаунта: статус, NS
    cf.py list <зона> [--type A]         записи зоны
    cf.py set <зона> <имя> <тип> <значение> [--proxied] [--ttl N] [--comment "…"] [--dry-run]
                                         создать или обновить (имя — «s1» или «s1.bogun.site», «@» — корень)
    cf.py delete <зона> <имя> <тип> [--content X] --yes
    cf.py check <зона>                   записи на наши серверы с оранжевым облаком, чужие IP,
                                         совпадает ли то, что видит мир (1.1.1.1), с зоной

Токен — CF_API_TOKEN из окружения или из файла CF_ENV_FILE
(по умолчанию D:\\CLOUDE\\_private\\cloudflare.env). Токен не печатается никогда.
"""
import argparse
import json
import os
import socket
import sys
import urllib.error
import urllib.parse
import urllib.request

API = "https://api.cloudflare.com/client/v4"
DEFAULT_ENV = r"D:\CLOUDE\_private\cloudflare.env"

# Свои серверы: записи на них должны быть «серыми» (proxied: false) — EdgeDeck
# проверяет, что A-запись смотрит на сервер, и сам выпускает сертификаты; через
# прокси Cloudflare это ломается.
OUR_IPS = {
    "132.145.132.80": "s1",
    "150.136.127.73": "s2",
    "150.136.40.217": "s3",
}


def env():
    vals = {}
    path = os.environ.get("CF_ENV_FILE", DEFAULT_ENV)
    if os.path.exists(path):
        for line in open(path, encoding="utf-8"):
            line = line.strip()
            if line and not line.startswith("#") and "=" in line:
                k, v = line.split("=", 1)
                vals[k.strip()] = v.strip()
    for k in ("CF_API_TOKEN", "CF_ACCOUNT_ID"):
        if os.environ.get(k):
            vals[k] = os.environ[k]
    if not vals.get("CF_API_TOKEN"):
        sys.exit(f"нет CF_API_TOKEN (ни в окружении, ни в {path})")
    return vals


CFG = None


def call(method, path, body=None, params=None):
    url = API + path
    if params:
        url += "?" + urllib.parse.urlencode(params)
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Authorization", "Bearer " + CFG["CF_API_TOKEN"])
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            out = json.load(r)
    except urllib.error.HTTPError as e:
        try:
            out = json.load(e)
        except Exception:
            sys.exit(f"Cloudflare ответил {e.code}")
    if not out.get("success"):
        errs = "; ".join(f"{x.get('code')}: {x.get('message')}" for x in out.get("errors", []))
        sys.exit(f"Cloudflare: {errs or 'ошибка'}")
    return out


def paged(path, params=None):
    params = dict(params or {}, per_page=100, page=1)
    while True:
        out = call("GET", path, params=params)
        yield from out["result"]
        info = out.get("result_info") or {}
        if params["page"] >= info.get("total_pages", 1):
            return
        params["page"] += 1


def zone_id(name):
    for z in paged("/zones", {"name": name}):
        return z["id"]
    sys.exit(f"зона {name} не найдена в аккаунте")


def fqdn(name, zone):
    if name in ("@", "", zone):
        return zone
    return name if name.endswith("." + zone) else f"{name}.{zone}"


def cmd_verify(_):
    acc = CFG.get("CF_ACCOUNT_ID")
    path = f"/accounts/{acc}/tokens/verify" if acc else "/user/tokens/verify"
    t = call("GET", path)["result"]
    print(f"токен: {t.get('status')}, истекает: {t.get('expires_on') or 'никогда'}")
    zones = list(paged("/zones"))
    print(f"видит зон: {len(zones)} — {', '.join(z['name'] for z in zones)}")
    if zones:
        try:
            call("GET", f"/zones/{zones[0]['id']}/dns_records", params={"per_page": 1})
            print("чтение DNS: да")
        except SystemExit as e:
            print(f"чтение DNS: нет ({e})")


def cmd_zones(_):
    for z in paged("/zones"):
        print(f"{z['name']:<24} {z['status']:<10} NS: {', '.join(z.get('name_servers', []))}  план: {z['plan']['name']}")


def records(zone):
    return list(paged(f"/zones/{zone_id(zone)}/dns_records"))


def cmd_list(a):
    for r in sorted(records(a.zone), key=lambda r: (r["name"], r["type"])):
        if a.type and r["type"] != a.type.upper():
            continue
        cloud = "оранж" if r.get("proxied") else "серое"
        where = OUR_IPS.get(r["content"], "")
        print(f"{r['type']:<6} {r['name']:<42} {r['content'][:60]:<40} {cloud:<6} ttl={r['ttl']:<5} {where}  {r.get('comment') or ''}")


def cmd_set(a):
    zid = zone_id(a.zone)
    name = fqdn(a.name, a.zone)
    typ = a.type.upper()
    body = {"type": typ, "name": name, "content": a.content, "ttl": a.ttl, "proxied": bool(a.proxied)}
    if typ not in ("A", "AAAA", "CNAME"):
        body.pop("proxied")
    if a.comment:
        body["comment"] = a.comment
    same = [r for r in paged(f"/zones/{zid}/dns_records", {"name": name, "type": typ})]
    if len(same) > 1:
        sys.exit(f"у {name} {typ} несколько записей — уточните через delete --content")
    if same and same[0]["content"] == a.content and bool(same[0].get("proxied")) == bool(a.proxied):
        print(f"= {typ} {name} → {a.content} (уже так)")
        return
    action = "обновлю" if same else "создам"
    print(f"{action}: {typ} {name} → {a.content} ({'оранжевое' if a.proxied else 'серое'} облако)")
    if a.dry_run:
        return
    if same:
        call("PUT", f"/zones/{zid}/dns_records/{same[0]['id']}", body)
    else:
        call("POST", f"/zones/{zid}/dns_records", body)
    print("готово")


def cmd_delete(a):
    if not a.yes:
        sys.exit("удаление записи — добавьте --yes")
    zid = zone_id(a.zone)
    name = fqdn(a.name, a.zone)
    rs = [r for r in paged(f"/zones/{zid}/dns_records", {"name": name, "type": a.type.upper()})
          if not a.content or r["content"] == a.content]
    if not rs:
        sys.exit("такой записи нет")
    for r in rs:
        call("DELETE", f"/zones/{zid}/dns_records/{r['id']}")
        print(f"удалил: {r['type']} {r['name']} → {r['content']}")


def resolve_public(name, typ):
    # Через DNS-over-HTTPS Cloudflare (1.1.1.1): то, что видит мир.
    url = "https://cloudflare-dns.com/dns-query?" + urllib.parse.urlencode({"name": name, "type": typ})
    req = urllib.request.Request(url, headers={"accept": "application/dns-json"})
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            ans = json.load(r).get("Answer", [])
    except Exception:
        return None
    want = {"A": 1, "AAAA": 28, "CNAME": 5, "MX": 15, "TXT": 16}[typ]
    return sorted(x["data"].strip('"').rstrip(".") for x in ans if x.get("type") == want)


def cmd_check(a):
    rs = records(a.zone)
    problems = 0
    z = next(paged("/zones", {"name": a.zone}))
    print(f"зона {a.zone}: статус {z['status']}, NS Cloudflare: {', '.join(z.get('name_servers', []))}")
    try:
        url = "https://cloudflare-dns.com/dns-query?" + urllib.parse.urlencode({"name": a.zone, "type": "NS"})
        with urllib.request.urlopen(urllib.request.Request(url, headers={"accept": "application/dns-json"}), timeout=15) as r:
            ns = sorted(x["data"].rstrip(".") for x in json.load(r).get("Answer", []) if x.get("type") == 2)
    except Exception:
        ns = []
    print(f"NS, которые видит мир: {', '.join(ns) or '?'}")
    if z["status"] != "active" or (ns and not set(ns) <= set(z.get("name_servers", []))):
        print("  ! мир ещё видит другие NS: либо у регистратора не сменили на NS Cloudflare, либо держится кеш (до суток после смены)")
        problems += 1
    for r in sorted(rs, key=lambda r: r["name"]):
        if r["type"] not in ("A", "AAAA", "CNAME", "MX", "TXT"):
            continue
        notes = []
        if r["type"] == "A" and r["content"] in OUR_IPS and r.get("proxied"):
            notes.append("оранжевое облако на наш сервер — EdgeDeck не выпустит сертификат, сделайте серым")
        if r["type"] == "A" and r["content"] not in OUR_IPS:
            notes.append("не наш сервер")
        if r["type"] in ("A", "AAAA", "CNAME") and not r.get("proxied"):
            seen = resolve_public(r["name"], r["type"])
            if seen is not None and seen and r["content"].rstrip(".") not in seen:
                notes.append(f"мир видит {', '.join(seen)} — ещё не разошлось или запись перекрыта")
        flag = "!" if any("облако" in n or "мир видит" in n for n in notes) else " "
        if flag == "!":
            problems += 1
        where = OUR_IPS.get(r["content"], "")
        print(f" {flag} {r['type']:<5} {r['name']:<40} {r['content'][:50]:<32} {where:<3} {'; '.join(notes)}")
    print(f"проблем: {problems}")


def main():
    global CFG
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    sub.add_parser("verify").set_defaults(fn=cmd_verify)
    sub.add_parser("zones").set_defaults(fn=cmd_zones)
    s = sub.add_parser("list"); s.add_argument("zone"); s.add_argument("--type"); s.set_defaults(fn=cmd_list)
    s = sub.add_parser("set")
    for x in ("zone", "name", "type", "content"):
        s.add_argument(x)
    s.add_argument("--proxied", action="store_true")
    s.add_argument("--ttl", type=int, default=1, help="1 — авто")
    s.add_argument("--comment")
    s.add_argument("--dry-run", action="store_true")
    s.set_defaults(fn=cmd_set)
    s = sub.add_parser("delete")
    for x in ("zone", "name", "type"):
        s.add_argument(x)
    s.add_argument("--content"); s.add_argument("--yes", action="store_true"); s.set_defaults(fn=cmd_delete)
    s = sub.add_parser("check"); s.add_argument("zone"); s.set_defaults(fn=cmd_check)
    a = p.parse_args()
    CFG = env()
    a.fn(a)


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    main()
