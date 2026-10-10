// Тонкая обёртка над API панели. Никаких библиотек: запросов мало,
// и лишняя зависимость в бандле здесь ничего не даёт.

export type Site = {
  file: string;
  addresses: string[];
  kind: string;
  targets: string[];
  paths: string[];
  managed: boolean;
  is_default: boolean;
  size_bytes: number;
};

export type SeenDomain = {
  host: string;
  first_seen: string;
  last_seen: string;
  hits: number;
  tls_asked: boolean;
  tls_allowed: boolean;
  points_here: boolean;
  zone: string;
  foreign: boolean; // чужая зона: панель не отвечает, сертификат не выпускается
};

export type Certificate = {
  domains: string[];
  issuer: string;
  not_before: string;
  not_after: string;
  days_left: number;
  self_signed: boolean;
};

export type Warning = {
  level: "red" | "yellow";
  title: string;
  detail: string;
  action?: string;
};

export type Overview = {
  server: {
    hostname: string;
    public_ip: string;
    version: string;
    load_avg: number;
    mem_total_mb: number;
    mem_used_mb: number;
    disk_total_gb: number;
    disk_used_gb: number;
    cpu_count: number;
  };
  sites: Site[];
  seen_domains: SeenDomain[];
  certificates: Certificate[];
  warnings: Warning[];
  caddy: { alive: boolean; message: string };
  // routes.yaml или файлы сайтов разошлись с последним применённым.
  drift?: { added: string[] | null; removed: string[] | null; changed: string[] | null; files: string[] | null };
};

export type ContainerPort = {
  private: number;
  public: number;
  type: string;
  bind_ip: string;
  published: boolean;
  loopback_only: boolean;
};

export type Container = {
  name: string;
  image: string;
  state: string;
  status: string;
  networks: string[];
  in_edge: boolean;
  ports: ContainerPort[];
  compose: { project: string; service: string; working_dir: string };
  suggested_port: number;
  reachability: "ready" | "needs_edge" | "host_network" | "not_running";
  hint: string;
};

export type HostPort = {
  port: number;
  proto: string;
  bind_ip: string;
  process: string;
  pid: number;
  cmdline: string;
  is_docker: boolean;
  loopback_only: boolean;
  guess: string;
  container: string;
  verdict: Verdict;
};

// Вердикт по порту: три слоя фильтрации, сведённые в один ответ.
export type Verdict = {
  level: "open" | "limited" | "closed" | "unknown";
  summary: string;
  detail: string;
  advice?: string;
  allowed?: string[] | null;
};

export type Settings = {
  server: { hostname: string; public_ip: string; version: string; edge_dir: string };
  telegram: { configured: boolean };
  firewall: { ok: boolean; status: string; ufw_works: boolean };
  caddy: { alive: boolean; message: string };
  agent: { ok: boolean; message: string };
  sessions: { token: string; created: string; last_seen: string; ip: string; user_agent: string }[] | null;
  watch: { interval: string; down_warn: string; cert_warn_days: number };
  password_fresh: boolean;
};

export type Event = {
  time: string;
  kind: string;
  actor: string;
  message: string;
};

// Публичная витрина: имя домена, «отвечает» и визитка, которую сайт сам о себе
// публикует (title, description, иконка, og:image).
export type PublicSite = {
  domain: string;
  kind: string;
  online: boolean;
  title?: string;
  description?: string;
  icon?: string;
  image?: string;
  zone: string;
  redirect_to?: string;
};

// ---- маршруты (этап 1) ----

export type Target = { kind: "container" | "host" | "external"; name?: string; port?: number; url?: string };

export type Route = {
  domain: string;
  aliases?: string[];
  path?: string;
  type: "proxy" | "static" | "redirect" | "acme_only" | "stub";
  target?: Target | null;
  redirect_to?: string;
  max_upload?: string;
  read_timeout?: string;
  security_headers?: boolean;
  hsts?: boolean;
  hide_upstream_csp?: boolean;
  csp?: string;
  allow_ips?: string[];
  deny_paths?: string[];
  comment?: string;
  // Возможности, без которых сайты раньше оставались «руками».
  headers?: { name: string; value: string }[];
  compress?: boolean;
  path_redirects?: { from: string; to: string; code?: number }[];
  acme_webroot?: string;
  flush?: boolean;
  dial_timeout?: string;
  auth?: { upstream: string; uri: string; copy_headers?: string[]; header_up?: { name: string; value: string }[] } | null;
};

export type RouteView = Route & { id: string; target_addr: string; zone: string };

export type ManualFile = {
  file: string;
  addresses: string[];
  targets: string[];
  can_import: boolean;
  why_not?: string;
  is_default: boolean;
  zone: string;
};

export type Change = { kind: "create" | "update" | "delete" | "unchanged"; file: string; before?: string; after?: string };

export type Plan = { changes: Change[]; warnings?: string[]; valid: boolean; error?: string };

export type ApplyResult = {
  applied: boolean;
  rolled_back: boolean;
  snapshot?: string;
  before: Record<string, string>;
  after: Record<string, string>;
  message: string;
  changes: Change[];
};

export type Snapshot = { name: string; time: string; actor: string; reason: string; files: number };

// ---- флот ----

export type FleetMember = { id: string; label: string; url: string; self: boolean };

export type FleetSummary = {
  id: string;
  label: string;
  server: Overview["server"];
  caddy: { alive: boolean; message: string };
  sites: PublicSite[] | null;
  warnings: Warning[] | null;
  certificates: { domains: string[]; days_left: number }[] | null;
  seen_unconfigured: number;
  foreign_zones: number;
  drift: boolean;
  time: string;
};

export type FleetServer = { member: FleetMember; ok: boolean; error?: string; summary?: FleetSummary };

export class Unauthorized extends Error {}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
  });
  if (res.status === 401) throw new Unauthorized("нужен вход");
  const text = await res.text();
  const data = text ? JSON.parse(text) : {};
  if (!res.ok) throw new Error(data.error ?? `ошибка ${res.status}`);
  return data as T;
}

export const api = {
  publicSites: () => request<{ sites: PublicSite[] }>("/api/public/sites"),
  session: () => request<{ authenticated: boolean }>("/api/session"),
  login: (password: string) =>
    request<{ ok: boolean }>("/api/login", { method: "POST", body: JSON.stringify({ password }) }),
  loginMethods: () => request<{ telegram: boolean }>("/api/login/methods"),
  sendCode: () => request<{ challenge: string }>("/api/login/telegram/send", { method: "POST" }),
  loginCode: (challenge: string, code: string) =>
    request<{ ok: boolean }>("/api/login/telegram", { method: "POST", body: JSON.stringify({ challenge, code }) }),
  logout: () => request<{ ok: boolean }>("/api/logout", { method: "POST" }),
  // Go отдаёт пустой список как null — приводим к [], чтобы экраны не падали.
  overview: () =>
    request<Overview>("/api/overview").then((o) => ({
      ...o,
      sites: o.sites ?? [],
      seen_domains: o.seen_domains ?? [],
      certificates: o.certificates ?? [],
      warnings: o.warnings ?? [],
    })),
  containers: () =>
    request<{ containers: Container[]; networks: { name: string }[]; compose_hint: string }>("/api/containers"),
  ports: () =>
    request<{ available: boolean; error?: string; ports: HostPort[]; firewall_ok?: boolean; firewall_status?: string }>(
      "/api/ports",
    ),
  settings: () => request<Settings>("/api/settings"),
  testTelegram: () =>
    request<{ ok: boolean; message: string }>("/api/settings/test-telegram", { method: "POST" }),
  changePassword: (current: string, next: string) =>
    request<{ ok: boolean }>("/api/password", { method: "POST", body: JSON.stringify({ current, new: next }) }),
  events: () => request<{ events: Event[] }>("/api/events"),
  checkDomain: (domain: string) =>
    request<{ domain: string; resolved: string[] | null; server_ip: string; points_here: boolean; message: string }>(
      "/api/check-domain",
      { method: "POST", body: JSON.stringify({ domain }) },
    ),
  probe: (host: string, port: number, hostHeader?: string) =>
    request<{
      target: string;
      tcp_open: boolean;
      http_status: number;
      server: string;
      redirect: string;
      has_csp: boolean;
      has_hsts: boolean;
      duration_ms: number;
      message: string;
    }>("/api/probe", { method: "POST", body: JSON.stringify({ host, port, host_header: hostHeader ?? "" }) }),
  connectEdge: (container: string, detach = false) =>
    request<{ ok: boolean; message: string }>("/api/connect-edge", {
      method: "POST",
      body: JSON.stringify({ container, detach }),
    }),
  routes: () => request<{ routes: RouteView[] | null; manual: ManualFile[] | null; file_path: string }>("/api/routes"),
  routePlan: (route: Route, id = "", adopt = "") =>
    request<Plan>("/api/routes/save", {
      method: "POST",
      body: JSON.stringify({ id, route, adopt, dry_run: true }),
    }),
  routeSave: (route: Route, id = "", adopt = "") =>
    request<ApplyResult>("/api/routes/save", { method: "POST", body: JSON.stringify({ id, route, adopt }) }),
  routeDelete: (id: string) =>
    request<ApplyResult>("/api/routes/delete", { method: "POST", body: JSON.stringify({ id }) }),
  routeImport: (file: string) =>
    request<ApplyResult>("/api/routes/import", { method: "POST", body: JSON.stringify({ file }) }),
  snapshots: () => request<{ snapshots: Snapshot[] | null }>("/api/snapshots"),
  rollback: (name: string) =>
    request<{ ok: boolean; message: string }>("/api/rollback", { method: "POST", body: JSON.stringify({ name }) }),
  fleet: () => request<{ enabled: boolean; self?: string; servers: FleetServer[] }>("/api/fleet"),
  fleetMembers: () => request<{ enabled: boolean; self?: string; members: FleetMember[] }>("/api/fleet/members"),
  applyPending: () => request<ApplyResult>("/api/routes/apply-pending", { method: "POST" }),
  discardPending: () => request<{ ok: boolean; message: string }>("/api/routes/discard", { method: "POST" }),
  forgetDomain: (domain: string) =>
    request<{ ok: boolean }>("/api/forget-domain", { method: "POST", body: JSON.stringify({ domain }) }),
  trustZone: (domain: string, trusted: boolean) =>
    request<{ ok: boolean; zone: string; message: string }>("/api/trust-zone", {
      method: "POST",
      body: JSON.stringify({ domain, trusted }),
    }),
};
