import { useEffect, useState } from "react";
import { api, type Overview } from "../api";
import { Dashboard } from "./Dashboard";
import { PortsView } from "./PortsView";
import { CertsView } from "./CertsView";
import { HistoryView } from "./HistoryView";
import { ContainersView } from "./ContainersView";

type Tab = "домены" | "цели" | "порты" | "сертификаты" | "история";
const tabs: Tab[] = ["домены", "цели", "порты", "сертификаты", "история"];

export function Shell({ onLogout }: { onLogout: () => void }) {
  const [tab, setTab] = useState<Tab>("домены");
  const [overview, setOverview] = useState<Overview | null>(null);
  const [error, setError] = useState("");

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
    return () => clearInterval(t);
  }, []);

  const s = overview?.server;

  return (
    <div className="mx-auto flex h-full max-w-6xl flex-col px-4 py-5">
      <header className="mb-5 flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="text-lg font-semibold">EdgeDeck</div>
        {s && (
          <div className="text-sm text-muted">
            {s.hostname} · {s.public_ip || "адрес неизвестен"}
          </div>
        )}
        <div className="ml-auto flex items-center gap-4 text-xs text-muted">
          {s && (
            <>
              <span>CPU {s.cpu_count} · нагрузка {s.load_avg.toFixed(2)}</span>
              <span>
                RAM {(s.mem_used_mb / 1024).toFixed(1)}/{(s.mem_total_mb / 1024).toFixed(1)} ГБ
              </span>
              <span>
                диск {s.disk_used_gb}/{s.disk_total_gb} ГБ
              </span>
              <span className="hidden sm:inline">{s.version}</span>
            </>
          )}
          <button
            onClick={async () => {
              await api.logout();
              onLogout();
            }}
            className="rounded-md border border-line px-2 py-1 hover:border-accent hover:text-text"
          >
            выйти
          </button>
        </div>
      </header>

      <nav className="mb-5 flex gap-1 overflow-x-auto">
        {tabs.map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={
              "rounded-lg px-3 py-1.5 text-sm capitalize " +
              (tab === t ? "bg-panel-2 text-text" : "text-muted hover:text-text")
            }
          >
            {t}
          </button>
        ))}
      </nav>

      {error && (
        <div className="mb-4 rounded-lg border border-bad/40 bg-bad/10 px-3 py-2 text-sm text-bad">{error}</div>
      )}

      <main className="flex-1 pb-8">
        {tab === "домены" && <Dashboard overview={overview} onChanged={reload} />}
        {tab === "цели" && <ContainersView />}
        {tab === "порты" && <PortsView />}
        {tab === "сертификаты" && <CertsView certs={overview?.certificates ?? []} />}
        {tab === "история" && <HistoryView />}
      </main>
    </div>
  );
}
