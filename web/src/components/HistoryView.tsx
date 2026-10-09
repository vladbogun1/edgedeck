import {
  Archive,
  CheckCircle2,
  Cable,
  KeyRound,
  LogIn,
  RotateCcw,
  ShieldCheck,
  ShieldX,
  Send,
  ShieldAlert,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api, type Event, type Snapshot } from "../api";
import { Empty, PageHeader, Section, SkeletonRows, cn } from "../ui";

const kinds: Record<string, { label: string; icon: LucideIcon; tone: "ok" | "warn" | "bad" | "accent" | "muted" }> = {
  login: { label: "вход", icon: LogIn, tone: "accent" },
  login_failed: { label: "неудачный вход", icon: XCircle, tone: "bad" },
  otp_sent: { label: "код в Telegram", icon: Send, tone: "muted" },
  password_changed: { label: "смена пароля", icon: KeyRound, tone: "accent" },
  network: { label: "сеть edge", icon: Cable, tone: "muted" },
  tls_ask_allowed: { label: "разрешён сертификат", icon: ShieldCheck, tone: "ok" },
  tls_ask_denied: { label: "отказ в сертификате", icon: ShieldX, tone: "warn" },
  trust_zone: { label: "своя зона", icon: ShieldAlert, tone: "accent" },
  apply: { label: "применено", icon: CheckCircle2, tone: "ok" },
  apply_failed: { label: "не применилось", icon: XCircle, tone: "bad" },
  rollback: { label: "откат", icon: RotateCcw, tone: "warn" },
  rollback_auto: { label: "автооткат", icon: RotateCcw, tone: "warn" },
};

export function HistoryView() {
  const [events, setEvents] = useState<Event[] | null>(null);
  const [snapshots, setSnapshots] = useState<Snapshot[] | null>(null);
  const [busy, setBusy] = useState("");
  const [note, setNote] = useState("");

  const load = useCallback(async () => {
    const [e, s] = await Promise.all([api.events(), api.snapshots()]);
    setEvents(e.events ?? []);
    setSnapshots(s.snapshots ?? []);
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <div className="space-y-9">
      <PageHeader title="История" subtitle="Снимки перед каждым изменением и журнал действий" />

      <Section
        title="Снимки состояния"
        count={snapshots?.length}
        hint={
          <>
            Снимок — обычные файлы в <code>~/edgedeck/data/snapshots</code>: их можно посмотреть и вернуть руками по SSH,
            даже если панель не работает.
          </>
        }
      >
        {note && <div className="callout mb-3">{note}</div>}
        {snapshots === null ? (
          <SkeletonRows rows={3} />
        ) : snapshots.length === 0 ? (
          <Empty icon={<Archive className="size-5" />} title="Снимков ещё нет">
            Они создаются перед каждым изменением маршрутов.
          </Empty>
        ) : (
          <div className="card rows overflow-hidden">
            {snapshots.map((s) => (
              <div key={s.name} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 sm:px-5">
                <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-white/5 text-muted">
                  <Archive className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm">{s.reason || "без описания"}</div>
                  <div className="truncate font-mono text-[11px] text-faint">
                    {s.name} · {s.files} {s.files === 1 ? "файл" : "файлов"} · {s.actor}
                  </div>
                </div>
                <button
                  disabled={busy === s.name}
                  onClick={async () => {
                    if (!confirm(`Вернуть состояние из снимка ${s.name}? Текущие файлы сайтов будут заменены.`)) return;
                    setBusy(s.name);
                    try {
                      const r = await api.rollback(s.name);
                      setNote(r.message);
                      await load();
                    } catch (e) {
                      setNote(e instanceof Error ? e.message : "не вышло");
                    } finally {
                      setBusy("");
                    }
                  }}
                  className="btn btn-ghost btn-sm"
                >
                  <RotateCcw className="size-3.5" />
                  Вернуть
                </button>
              </div>
            ))}
          </div>
        )}
      </Section>

      <Section title="Журнал" count={events?.length}>
        {!events ? (
          <SkeletonRows rows={6} />
        ) : events.length === 0 ? (
          <Empty icon={<Archive className="size-5" />} title="Пока ничего не произошло" />
        ) : (
          <ol className="card relative overflow-hidden py-2">
            <span className="absolute top-4 bottom-4 left-[33px] w-px bg-line sm:left-[37px]" aria-hidden />
            {events.map((e, i) => {
              const k = kinds[e.kind] ?? { label: e.kind, icon: CheckCircle2, tone: "muted" as const };
              return (
                <li key={i} className="relative flex items-start gap-3 px-4 py-2.5 sm:px-5">
                  <span
                    className={cn(
                      "relative z-10 grid size-7 shrink-0 place-items-center rounded-full border bg-panel",
                      k.tone === "ok" && "border-ok/30 text-ok",
                      k.tone === "warn" && "border-warn/30 text-warn",
                      k.tone === "bad" && "border-bad/30 text-bad",
                      k.tone === "accent" && "border-accent/30 text-accent-hi",
                      k.tone === "muted" && "border-line text-faint",
                    )}
                  >
                    <k.icon className="size-3.5" />
                  </span>
                  <div className="min-w-0 flex-1 pt-0.5">
                    <div className="flex flex-wrap items-baseline gap-x-2">
                      <span className="text-[13px] font-medium">{k.label}</span>
                      <span className="text-sm text-muted">{e.message}</span>
                    </div>
                    <div className="mt-0.5 font-mono text-[11px] text-faint">
                      {new Date(e.time).toLocaleString("ru-RU")} · {e.actor}
                    </div>
                  </div>
                </li>
              );
            })}
          </ol>
        )}
      </Section>
    </div>
  );
}
