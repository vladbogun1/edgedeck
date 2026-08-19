import { useCallback, useEffect, useState } from "react";
import { api, type Event, type Snapshot } from "../api";

const labels: Record<string, string> = {
  login: "вход",
  login_failed: "неудачный вход",
  password_changed: "смена пароля",
  network: "сеть edge",
  tls_ask_allowed: "разрешён сертификат",
  tls_ask_denied: "отказ в сертификате",
  apply: "применено",
  apply_failed: "не применилось",
  rollback: "откат",
  rollback_auto: "автоматический откат",
};

export function HistoryView() {
  const [events, setEvents] = useState<Event[] | null>(null);
  const [snapshots, setSnapshots] = useState<Snapshot[]>([]);
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
    <div className="space-y-6">
      <section>
        <h2 className="mb-3 text-sm font-medium tracking-wide text-muted uppercase">Снимки состояния</h2>
        {note && <div className="mb-2 rounded-lg border border-line bg-panel px-3 py-2 text-sm">{note}</div>}
        {snapshots.length === 0 ? (
          <div className="rounded-xl border border-line bg-panel p-4 text-sm text-muted">
            Снимков ещё нет — они создаются перед каждым изменением маршрутов.
          </div>
        ) : (
          <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
            {snapshots.map((s) => (
              <div key={s.name} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-3 text-sm">
                <span className="font-mono text-xs">{s.name}</span>
                <span className="text-muted">{s.reason || "без описания"}</span>
                <span className="text-xs text-muted">
                  {s.files} {s.files === 1 ? "файл" : "файлов"} · {s.actor}
                </span>
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
                  className="ml-auto text-xs text-accent underline disabled:opacity-40"
                >
                  вернуть
                </button>
              </div>
            ))}
          </div>
        )}
        <p className="mt-2 text-xs text-muted">
          Снимок — обычные файлы в <code>~/edgedeck/data/snapshots</code>: их можно посмотреть и вернуть руками по SSH,
          даже если панель не работает.
        </p>
      </section>

      <section>
        <h2 className="mb-3 text-sm font-medium tracking-wide text-muted uppercase">Журнал</h2>
        {!events ? (
          <div className="text-muted">загружаю…</div>
        ) : events.length === 0 ? (
          <div className="rounded-xl border border-line bg-panel p-4 text-sm text-muted">Пока ничего не произошло.</div>
        ) : (
          <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
            {events.map((e, i) => (
              <div key={i} className="flex flex-wrap items-baseline gap-x-3 px-4 py-2 text-sm">
                <span className="w-40 text-xs text-muted">{new Date(e.time).toLocaleString("ru-RU")}</span>
                <span
                  className={
                    "w-44 text-xs " +
                    (e.kind.includes("failed") || e.kind.includes("denied") || e.kind.includes("rollback")
                      ? "text-warn"
                      : "text-muted")
                  }
                >
                  {labels[e.kind] ?? e.kind}
                </span>
                <span>{e.message}</span>
                <span className="ml-auto text-xs text-muted">{e.actor}</span>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
