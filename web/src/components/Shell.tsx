import { AnimatePresence, motion } from "framer-motion";
import {
  Boxes,
  LayoutGrid,
  Globe2,
  History,
  LogOut,
  Network,
  ServerCog,
  Settings2,
  ShieldCheck,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useState } from "react";
import { api, type FleetMember, type Overview } from "../api";
import { ErrorBoundary } from "../ErrorBoundary";
import { Meter, cn } from "../ui";
import { CertsView } from "./CertsView";
import { ContainersView } from "./ContainersView";
import { Dashboard } from "./Dashboard";
import { FleetView } from "./FleetView";
import { HistoryView } from "./HistoryView";
import { PortsView } from "./PortsView";
import { SettingsView } from "./SettingsView";

type Tab = "fleet" | "domains" | "targets" | "ports" | "certs" | "history" | "settings";

const FLEET_NAV = { id: "fleet" as Tab, label: "Флот", icon: LayoutGrid };

const NAV: { id: Tab; label: string; icon: LucideIcon }[] = [
  { id: "domains", label: "Домены", icon: Globe2 },
  { id: "targets", label: "Цели", icon: Boxes },
  { id: "ports", label: "Порты", icon: Network },
  { id: "certs", label: "Сертификаты", icon: ShieldCheck },
  { id: "history", label: "История", icon: History },
  { id: "settings", label: "Настройки", icon: Settings2 },
];

// Раздел живёт в адресе (#ports), чтобы ссылку на него можно было открыть
// заново и «назад» в браузере работал.
function tabFromHash(): Tab {
  const h = window.location.hash.replace("#", "") as Tab;
  return h === "fleet" || NAV.some((n) => n.id === h) ? h : "domains";
}

export function Shell({ onLogout }: { onLogout: () => void }) {
  const [tab, setTabState] = useState<Tab>(tabFromHash);
  const [overview, setOverview] = useState<Overview | null>(null);
  const [error, setError] = useState("");
  const [fleet, setFleet] = useState<FleetMember[]>([]);

  function setTab(t: Tab) {
    if (t !== tab) window.location.hash = t;
    setTabState(t);
  }

  async function reload() {
    try {
      setOverview(await api.overview());
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "не загрузилось");
    }
  }

  useEffect(() => {
    void reload();
    const t = setInterval(() => void reload(), 15000);
    api
      .fleetMembers()
      .then((f) => setFleet(f.enabled ? f.members : []))
      .catch(() => {});
    const onHash = () => setTabState(tabFromHash());
    window.addEventListener("hashchange", onHash);
    return () => {
      clearInterval(t);
      window.removeEventListener("hashchange", onHash);
    };
  }, []);

  async function logout() {
    await api.logout();
    onLogout();
  }

  // Счётчики «требует внимания» на пунктах меню.
  const badges: Partial<Record<Tab, { n: number; strong: boolean }>> = {};
  if (overview) {
    const own = overview.seen_domains.filter((d) => !d.foreign).length;
    const warn = overview.warnings.length;
    if (own + warn > 0) badges.domains = { n: own + warn, strong: warn > 0 };
    const expiring = overview.certificates.filter((c) => !c.self_signed && c.days_left <= 21).length;
    if (expiring > 0) badges.certs = { n: expiring, strong: true };
  }

  const nav = fleet.length > 0 ? [FLEET_NAV, ...NAV] : NAV;
  const current = nav.find((n) => n.id === tab) ?? NAV[0];

  return (
    <div className="flex min-h-dvh">
      {/* Боковое меню (десктоп) */}
      <aside className="sticky top-0 hidden h-dvh w-[260px] shrink-0 flex-col border-r border-line bg-bg-2/70 px-3 py-5 backdrop-blur-xl lg:flex">
        <div className="mb-7 flex items-center gap-2.5 px-3">
          <span className="grid size-8 place-items-center rounded-[10px] bg-[image:var(--accent-grad)] text-[#04101f] shadow-[var(--glow-accent)]">
            <ServerCog className="size-4.5" strokeWidth={2.4} />
          </span>
          <div className="leading-tight">
            <div className="font-display text-[16px] font-bold tracking-tight">EdgeDeck</div>
            <div className="font-mono text-[10.5px] text-faint">{overview?.server.version ?? "…"}</div>
          </div>
        </div>

        {fleet.length > 0 && <ServerSwitcher members={fleet} />}

        <div className="eyebrow mb-2 px-3">Сервер</div>
        <nav className="space-y-0.5">
          {nav.map((n) => (
            <NavItem key={n.id} item={n} active={tab === n.id} badge={badges[n.id]} onClick={() => setTab(n.id)} />
          ))}
        </nav>

        <div className="mt-auto space-y-3">
          {overview && <ServerCard o={overview} />}
          <button
            onClick={logout}
            className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm text-muted transition-colors hover:bg-bad/10 hover:text-[#fecaca]"
          >
            <LogOut className="size-4" />
            Выйти
          </button>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        {/* Верхняя полоса (телефон и планшет) */}
        <header className="sticky top-0 z-30 border-b border-line bg-ink/75 backdrop-blur-xl lg:hidden">
          <div className="flex items-center gap-3 px-4 py-3">
            <span className="grid size-7 place-items-center rounded-lg bg-[image:var(--accent-grad)] text-[#04101f]">
              <ServerCog className="size-4" strokeWidth={2.4} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="font-display text-sm font-bold">{current.label}</div>
              {overview && (
                <div className="truncate font-mono text-[10.5px] text-faint">
                  {overview.server.hostname} · {overview.server.public_ip}
                </div>
              )}
            </div>
            <button onClick={logout} aria-label="Выйти" className="btn btn-ghost btn-icon !size-9">
              <LogOut className="size-4" />
            </button>
          </div>
          <nav className="thin-scroll flex gap-1 overflow-x-auto px-3 pb-2">
            {nav.map((n) => {
              const active = tab === n.id;
              const b = badges[n.id];
              return (
                <button
                  key={n.id}
                  onClick={() => setTab(n.id)}
                  className={cn(
                    "relative flex h-9 shrink-0 items-center gap-1.5 rounded-lg px-3 text-[13px] font-medium transition-colors",
                    active ? "text-accent-hi" : "text-muted",
                  )}
                >
                  {active && (
                    <motion.span
                      layoutId="mtab"
                      transition={{ type: "spring", stiffness: 420, damping: 36 }}
                      className="absolute inset-0 rounded-lg border border-accent/30 bg-accent/10"
                    />
                  )}
                  <n.icon className="relative size-4" />
                  <span className="relative">{n.label}</span>
                  {b && <Count n={b.n} strong={b.strong} />}
                </button>
              );
            })}
          </nav>
        </header>

        <main className="mx-auto w-full max-w-6xl flex-1 px-4 pt-6 pb-16 sm:px-6 lg:px-10 lg:pt-10">
          {error && (
            <div role="alert" className="callout mb-5 text-bad [--tone:var(--color-bad)]">
              {error}
            </div>
          )}
          <AnimatePresence mode="wait">
            <motion.div
              key={tab}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0, transition: { duration: 0.26, ease: [0.22, 1, 0.36, 1] } }}
              exit={{ opacity: 0, y: -4, transition: { duration: 0.12 } }}
            >
              <ErrorBoundary resetKey={tab}>
              {tab === "fleet" && <FleetView />}
              {tab === "domains" && <Dashboard overview={overview} onChanged={reload} />}
              {tab === "targets" && <ContainersView />}
              {tab === "ports" && <PortsView />}
              {tab === "certs" && <CertsView certs={overview?.certificates ?? null} />}
              {tab === "history" && <HistoryView />}
              {tab === "settings" && <SettingsView />}
              </ErrorBoundary>
            </motion.div>
          </AnimatePresence>
        </main>
      </div>
    </div>
  );
}

// Переключатель серверов: своя панель подсвечена, соседняя открывается по её адресу.
function ServerSwitcher({ members }: { members: FleetMember[] }) {
  return (
    <div className="mb-6 px-1">
      <div className="eyebrow mb-2 px-2">Серверы</div>
      <div className="grid grid-cols-3 gap-1 rounded-xl border border-line bg-black/25 p-1">
        {members.map((m) =>
          m.self ? (
            <span
              key={m.id}
              title={m.label}
              className="rounded-lg border border-accent/35 bg-accent/12 py-1.5 text-center font-mono text-[12px] font-semibold text-accent-hi"
            >
              {m.id}
            </span>
          ) : (
            <a
              key={m.id}
              href={m.url}
              title={`${m.label} — открыть панель`}
              className="rounded-lg py-1.5 text-center font-mono text-[12px] text-muted transition-colors hover:bg-white/5 hover:text-text"
            >
              {m.id}
            </a>
          ),
        )}
      </div>
    </div>
  );
}

function NavItem({
  item,
  active,
  badge,
  onClick,
}: {
  item: (typeof NAV)[number];
  active: boolean;
  badge?: { n: number; strong: boolean };
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      aria-current={active ? "page" : undefined}
      className={cn(
        "group relative flex h-10 w-full items-center gap-3 rounded-lg px-3 text-[14px] font-medium transition-colors",
        active ? "text-text" : "text-muted hover:bg-white/[0.04] hover:text-text",
      )}
    >
      {active && (
        <motion.span
          layoutId="nav-active"
          transition={{ type: "spring", stiffness: 420, damping: 36 }}
          className="absolute inset-0 rounded-lg border border-white/[0.07] bg-gradient-to-r from-accent/15 to-transparent"
        >
          <span className="absolute inset-y-2 left-0 w-[2px] rounded-full bg-[image:var(--accent-grad)] shadow-[var(--glow-accent)]" />
        </motion.span>
      )}
      <item.icon
        className={cn("relative size-[18px] transition-colors", active ? "text-accent-hi" : "text-faint group-hover:text-muted")}
        strokeWidth={active ? 2.25 : 2}
      />
      <span className="relative">{item.label}</span>
      {badge && (
        <span className="relative ml-auto">
          <Count n={badge.n} strong={badge.strong} />
        </span>
      )}
    </button>
  );
}

function Count({ n, strong }: { n: number; strong: boolean }) {
  return (
    <span
      className={cn(
        "grid h-[18px] min-w-[18px] place-items-center rounded-full px-1.5 font-mono text-[10.5px] font-semibold",
        strong ? "bg-warn text-[#1a1203]" : "bg-white/10 text-muted",
      )}
    >
      {n}
    </span>
  );
}

// Карточка сервера внизу меню: имя, адрес и три полоски нагрузки.
function ServerCard({ o }: { o: Overview }) {
  const s = o.server;
  const loadPct = s.cpu_count > 0 ? s.load_avg / s.cpu_count : 0;
  return (
    <div className="card p-3.5">
      <div className="flex items-center gap-2">
        <span className={cn("dot dot-live", o.caddy.alive ? "text-ok" : "text-bad")} />
        <span className="truncate text-[13px] font-semibold">{s.hostname}</span>
      </div>
      <div className="mt-0.5 font-mono text-[11px] text-faint">{s.public_ip || "адрес неизвестен"}</div>
      <div className="mt-3 space-y-2.5">
        <MeterRow label="CPU" value={`${s.load_avg.toFixed(2)} / ${s.cpu_count}`}>
          <Meter value={loadPct} max={1} />
        </MeterRow>
        <MeterRow label="RAM" value={`${(s.mem_used_mb / 1024).toFixed(1)} / ${(s.mem_total_mb / 1024).toFixed(0)} ГБ`}>
          <Meter value={s.mem_used_mb} max={s.mem_total_mb} />
        </MeterRow>
        <MeterRow label="Диск" value={`${s.disk_used_gb} / ${s.disk_total_gb} ГБ`}>
          <Meter value={s.disk_used_gb} max={s.disk_total_gb} />
        </MeterRow>
      </div>
    </div>
  );
}

function MeterRow({ label, value, children }: { label: string; value: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1 flex justify-between text-[11px]">
        <span className="text-muted">{label}</span>
        <span className="font-mono text-faint">{value}</span>
      </div>
      {children}
    </div>
  );
}
