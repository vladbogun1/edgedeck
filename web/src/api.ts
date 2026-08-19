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
};

export type Event = {
  time: string;
  kind: string;
  actor: string;
  message: string;
};

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
  session: () => request<{ authenticated: boolean }>("/api/session"),
  login: (password: string) =>
    request<{ ok: boolean }>("/api/login", { method: "POST", body: JSON.stringify({ password }) }),
  logout: () => request<{ ok: boolean }>("/api/logout", { method: "POST" }),
  overview: () => request<Overview>("/api/overview"),
  containers: () => request<{ containers: Container[]; networks: { name: string }[] }>("/api/containers"),
  ports: () => request<{ available: boolean; error?: string; ports: HostPort[] }>("/api/ports"),
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
  forgetDomain: (domain: string) =>
    request<{ ok: boolean }>("/api/forget-domain", { method: "POST", body: JSON.stringify({ domain }) }),
};
