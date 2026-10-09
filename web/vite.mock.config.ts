// Предпросмотр интерфейса без сервера: `npx vite --config vite.mock.config.ts`.
// MOCK_PUBLIC=путь/к/public.json — настоящая витрина (снять с /api/public/sites);
// MOCK_AUTH=1 — сразу «залогинен», чтобы смотреть админку. Остальное API —
// правдоподобные тестовые данные.
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwind from "@tailwindcss/vite";
import { readFileSync } from "node:fs";

const now = Date.now();
const iso = (minAgo: number) => new Date(now - minAgo * 60000).toISOString();
const LE = "Let's Encrypt";

const data: Record<string, unknown> = {
  "/api/session": { authenticated: process.env.MOCK_AUTH === "1" },
  "/api/login/methods": { telegram: true },
  "/api/overview": {
    server: {
      hostname: "instance-20230603-1117",
      public_ip: "132.145.132.80",
      version: "sha-5903f05",
      load_avg: 0.62,
      mem_total_mb: 24000,
      mem_used_mb: 15100,
      disk_total_gb: 96,
      disk_used_gb: 61,
      cpu_count: 4,
    },
    sites: [],
    seen_domains: [
      { host: "mail.maxsolch.com", first_seen: iso(9000), last_seen: iso(12), hits: 1259, tls_asked: true, tls_allowed: true, points_here: true, zone: "maxsolch.com", foreign: false },
      { host: "pop.maxsolch.com", first_seen: iso(9000), last_seen: iso(90), hits: 698, tls_asked: true, tls_allowed: true, points_here: true, zone: "maxsolch.com", foreign: false },
      { host: "webdisk.amzcomverif.kozow.com", first_seen: iso(9000), last_seen: iso(3), hits: 7116, tls_asked: true, tls_allowed: false, points_here: true, zone: "amzcomverif.kozow.com", foreign: true },
      { host: "cpanel.amzcomverif.kozow.com", first_seen: iso(9000), last_seen: iso(5), hits: 1845, tls_asked: true, tls_allowed: false, points_here: true, zone: "amzcomverif.kozow.com", foreign: true },
      { host: "132-145-132-80.cprapid.com", first_seen: iso(9000), last_seen: iso(30), hits: 3668, tls_asked: true, tls_allowed: false, points_here: true, zone: "cprapid.com", foreign: true },
      { host: "www.google.com", first_seen: iso(9000), last_seen: iso(300), hits: 12, tls_asked: true, tls_allowed: false, points_here: false, zone: "google.com", foreign: true },
    ],
    certificates: [
      { domains: ["maxsolch.com", "www.maxsolch.com"], issuer: LE, not_before: iso(30 * 1440), not_after: iso(-60 * 1440), days_left: 60, self_signed: false },
      { domains: ["tgdesk.tech", "www.tgdesk.tech"], issuer: LE, not_before: iso(70 * 1440), not_after: iso(-19 * 1440), days_left: 19, self_signed: false },
      { domains: ["bogun.site"], issuer: LE, not_before: iso(10 * 1440), not_after: iso(-80 * 1440), days_left: 80, self_signed: false },
      { domains: ["chisetup.com.ua"], issuer: LE, not_before: iso(85 * 1440), not_after: iso(-5 * 1440), days_left: 5, self_signed: false },
      { domains: ["132.145.132.80"], issuer: "EdgeDeck", not_before: iso(10 * 1440), not_after: iso(-3000 * 1440), days_left: 3000, self_signed: true },
    ],
    warnings: [
      {
        level: "yellow",
        title: "Сертификат tgdesk.tech кончается через 19 дн.",
        detail: "Caddy продлевает за 30 дней — значит продлению что-то мешает.",
        action: "проверьте, что домен смотрит на этот сервер",
      },
    ],
    caddy: { alive: true, message: "" },
  },
  "/api/routes": {
    routes: [
      { id: "maxsolch.com/", domain: "maxsolch.com", aliases: ["www.maxsolch.com"], path: "/", type: "static", target_addr: "", security_headers: true },
      { id: "tgdesk.tech/", domain: "tgdesk.tech", aliases: ["www.tgdesk.tech"], path: "/", type: "proxy", target: { kind: "container", name: "twcrm-frontend", port: 80 }, target_addr: "twcrm-frontend:80", hsts: true },
    ],
    manual: [
      { file: "bogun.site.caddy", addresses: ["bogun.site", "www.bogun.site"], targets: ["jarvis:4000", "dozzle:8080", "vaultwarden:80"], can_import: false, why_not: "несколько целей по путям и forward_auth", is_default: false },
      { file: "bots.bogun.site.caddy", addresses: ["bots.bogun.site"], targets: ["apiary-web:80"], can_import: true, is_default: false },
      { file: "chisetup.com.ua.caddy", addresses: ["chisetup.com.ua"], targets: ["tgshop_v2_gateway_site:80"], can_import: false, why_not: "свой сниппет и три домена в файле", is_default: false },
      { file: "_default.caddy", addresses: [":80", "https://"], targets: ["edgedeck:4000"], can_import: false, is_default: true },
    ],
    file_path: "/edge/routes.yaml",
  },
  "/api/containers": {
    compose_hint: "networks:\n  edge:\n    external: true",
    networks: [{ name: "edge" }],
    containers: [
      { name: "twcrm-frontend", image: "ghcr.io/vladbogun1/twcrm-frontend:1.4.2", state: "running", status: "Up 4 days", networks: ["edge", "twcrm_default"], in_edge: true, ports: [{ private: 80, public: 0, type: "tcp", bind_ip: "", published: false, loopback_only: false }], compose: { project: "twcrm", service: "frontend", working_dir: "/opt/twcrm" }, suggested_port: 80, reachability: "ready", hint: "" },
      { name: "jarvis", image: "jarvis:latest", state: "running", status: "Up 9 days", networks: ["jarvis_default", "root_website_hubnet"], in_edge: false, ports: [{ private: 4000, public: 0, type: "tcp", bind_ip: "", published: false, loopback_only: false }], compose: { project: "jarvis", service: "jarvis", working_dir: "/home/ubuntu/JARVIS" }, suggested_port: 4000, reachability: "needs_edge", hint: "Caddy видит его через сеть root_website_hubnet, панель — нет" },
      { name: "tgshop_v2_backend", image: "maxsolch-v2-backend:3.19.2", state: "running", status: "Up 2 days (healthy)", networks: ["maxsolch-v2_tgshop"], in_edge: false, ports: [{ private: 8080, public: 0, type: "tcp", bind_ip: "", published: false, loopback_only: false }, { private: 5005, public: 5005, type: "tcp", bind_ip: "127.0.0.1", published: true, loopback_only: true }], compose: { project: "maxsolch-v2", service: "backend", working_dir: "" }, suggested_port: 8080, reachability: "needs_edge", hint: "" },
      { name: "vaultwarden", image: "vaultwarden/server:1.32.0", state: "exited", status: "Exited (0) 3 days ago", networks: [], in_edge: false, ports: [], compose: { project: "", service: "", working_dir: "" }, suggested_port: 0, reachability: "not_running", hint: "" },
    ],
  },
  "/api/ports": {
    available: true,
    firewall_ok: true,
    firewall_status: "снимок свежий, 12 с назад",
    ports: [
      { port: 22, proto: "tcp", bind_ip: "0.0.0.0", process: "", pid: 812, cmdline: "", is_docker: false, loopback_only: false, guess: "SSH", container: "", verdict: { level: "open", summary: "видно из интернета", detail: "INPUT разрешает 22/tcp до перехода в ufw.", advice: "это SSH — так и должно быть, но лучше ключи и fail2ban" } },
      { port: 80, proto: "tcp", bind_ip: "0.0.0.0", process: "", pid: 0, cmdline: "", is_docker: true, loopback_only: false, guess: "", container: "edge_caddy", verdict: { level: "open", summary: "видно из интернета", detail: "docker-порт, правил в DOCKER-USER нет" } },
      { port: 9443, proto: "tcp", bind_ip: "0.0.0.0", process: "", pid: 0, cmdline: "", is_docker: true, loopback_only: false, guess: "", container: "portainer", verdict: { level: "limited", summary: "только со своих адресов", detail: "DOCKER-USER пускает 2 адреса" } },
      { port: 3306, proto: "tcp", bind_ip: "127.0.0.1", process: "", pid: 0, cmdline: "", is_docker: true, loopback_only: true, guess: "", container: "tgshop_v2_mysql", verdict: { level: "closed", summary: "только с хоста", detail: "слушает 127.0.0.1" } },
    ],
  },
  "/api/events": {
    events: [
      { time: iso(2), kind: "login", actor: "93.170.4.18", message: "вход в панель по коду из Telegram" },
      { time: iso(3), kind: "otp_sent", actor: "93.170.4.18", message: "отправлен код входа в Telegram" },
      { time: iso(60), kind: "trust_zone", actor: "93.170.4.18", message: "зона example.org отмечена своей" },
      { time: iso(240), kind: "apply", actor: "93.170.4.18", message: "применено: tgdesk.tech" },
      { time: iso(600), kind: "tls_ask_denied", actor: "caddy", message: "лимит новых доменов в час" },
      { time: iso(900), kind: "login_failed", actor: "45.148.10.32", message: "неудачная попытка входа" },
      { time: iso(2000), kind: "rollback_auto", actor: "panel", message: "сайт перестал отвечать после применения — вернул как было" },
    ],
  },
  "/api/snapshots": {
    snapshots: [
      { name: "2026-10-09T18-11-02", time: iso(240), actor: "93.170.4.18", reason: "сохранение маршрута tgdesk.tech", files: 2 },
      { name: "2026-10-05T20-35-40", time: iso(6000), actor: "import", reason: "перенос с nginx", files: 7 },
    ],
  },
  "/api/settings": {
    server: { hostname: "instance-20230603-1117", public_ip: "132.145.132.80", version: "sha-5903f05", edge_dir: "/edge" },
    telegram: { configured: true },
    firewall: { ok: true, status: "снимок свежий, 12 с назад", ufw_works: false },
    caddy: { alive: true, message: "v2.11.6" },
    agent: { ok: true, message: "docker 29.7.2, aarch64" },
    sessions: [{ token: "a1b2…9f", created: iso(2), last_seen: iso(1), ip: "93.170.4.18", user_agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/141.0 Safari/537.36" }],
    watch: { interval: "1 минута", down_warn: "5 минут", cert_warn_days: 14 },
    password_fresh: true,
  },
};

function mock(): Plugin {
  return {
    name: "edgedeck-mock",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const path = (req.url ?? "").split("?")[0];
        if (!path.startsWith("/api/")) return next();
        let body: unknown = data[path];
        if (path === "/api/public/sites") {
          body = process.env.MOCK_PUBLIC ? JSON.parse(readFileSync(process.env.MOCK_PUBLIC, "utf8")) : { sites: [] };
        }
        if (body === undefined) body = { ok: true, challenge: "x", message: "ок" };
        res.setHeader("Content-Type", "application/json");
        setTimeout(() => res.end(JSON.stringify(body)), 250);
      });
    },
  };
}

export default defineConfig({ plugins: [react(), tailwind(), mock()], server: { port: 5174 } });
