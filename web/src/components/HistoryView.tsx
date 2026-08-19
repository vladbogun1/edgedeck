import { useEffect, useState } from "react";
import { api, type Event } from "../api";

const labels: Record<string, string> = {
  login: "вход",
  login_failed: "неудачный вход",
  password_changed: "смена пароля",
  network: "сеть edge",
  tls_ask_allowed: "разрешён сертификат",
  tls_ask_denied: "отказ в сертификате",
};

export function HistoryView() {
  const [events, setEvents] = useState<Event[] | null>(null);

  useEffect(() => {
    void (async () => setEvents((await api.events()).events))();
  }, []);

  if (!events) return <div className="text-muted">загружаю…</div>;
  if (events.length === 0) {
    return <div className="rounded-xl border border-line bg-panel p-4 text-sm text-muted">Пока ничего не произошло.</div>;
  }

  return (
    <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
      {events.map((e, i) => (
        <div key={i} className="flex flex-wrap items-baseline gap-x-3 px-4 py-2 text-sm">
          <span className="w-40 text-xs text-muted">{new Date(e.time).toLocaleString("ru-RU")}</span>
          <span className={"w-44 text-xs " + (e.kind.includes("failed") || e.kind.includes("denied") ? "text-warn" : "text-muted")}>
            {labels[e.kind] ?? e.kind}
          </span>
          <span>{e.message}</span>
          <span className="ml-auto text-xs text-muted">{e.actor}</span>
        </div>
      ))}
    </div>
  );
}
