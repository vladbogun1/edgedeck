import { useEffect, useState } from "react";
import { api, type Container } from "../api";

// «Цели» — то, на что вообще можно направить домен: контейнеры с их сетями и
// портами. Здесь же видно главную причину 502 на старых серверах: контейнер не
// подключён к сети прокси (DESIGN.md §2.3).
export function ContainersView() {
  const [list, setList] = useState<Container[] | null>(null);
  const [error, setError] = useState("");
  const [probe, setProbe] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");

  async function reload() {
    try {
      const data = await api.containers();
      setList(data.containers);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "не загрузилось");
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  if (error) return <div className="rounded-xl border border-bad/40 bg-bad/10 p-4 text-sm text-bad">{error}</div>;
  if (!list) return <div className="text-muted">загружаю…</div>;

  return (
    <div className="space-y-3">
      {list.map((c) => (
        <div key={c.name} className="rounded-xl border border-line bg-panel p-4">
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <span className="font-medium">{c.name}</span>
            <span className="text-xs text-muted">{c.image}</span>
            <Badge state={c.reachability} />
            <span className="ml-auto text-xs text-muted">{c.status}</span>
          </div>

          <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
            <span>сети: {c.networks.join(", ") || "нет"}</span>
            {c.compose.project && (
              <span>
                compose: {c.compose.project}
                {c.compose.working_dir && <span className="ml-1 font-mono">({c.compose.working_dir})</span>}
              </span>
            )}
          </div>

          {c.ports.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-2 text-xs">
              {c.ports.map((p) => (
                <span
                  key={`${p.private}/${p.type}/${p.bind_ip}`}
                  className={
                    "rounded-md border px-2 py-0.5 " +
                    (p.private === c.suggested_port ? "border-accent text-accent" : "border-line text-muted")
                  }
                  title={
                    p.loopback_only
                      ? "опубликован только на 127.0.0.1: с хоста доступен, из другого контейнера — нет"
                      : p.published
                        ? `опубликован на ${p.bind_ip}:${p.public}`
                        : "виден только внутри docker-сети"
                  }
                >
                  {p.private}/{p.type}
                  {p.published && ` → ${p.bind_ip}:${p.public}`}
                  {p.loopback_only && " (только с хоста)"}
                </span>
              ))}
            </div>
          )}

          {c.hint && <div className="mt-2 text-xs text-warn">{c.hint}</div>}

          <div className="mt-3 flex flex-wrap items-center gap-2">
            {c.reachability === "needs_edge" && (
              <button
                disabled={busy === c.name}
                onClick={async () => {
                  setBusy(c.name);
                  try {
                    const r = await api.connectEdge(c.name);
                    setProbe((p) => ({ ...p, [c.name]: r.message }));
                    await reload();
                  } catch (e) {
                    setProbe((p) => ({ ...p, [c.name]: e instanceof Error ? e.message : "не вышло" }));
                  } finally {
                    setBusy("");
                  }
                }}
                className="rounded-md border border-accent px-2 py-1 text-xs text-accent disabled:opacity-40"
              >
                подключить к сети edge
              </button>
            )}
            {c.suggested_port > 0 && (
              <button
                disabled={busy === c.name}
                onClick={async () => {
                  setBusy(c.name);
                  try {
                    const r = await api.probe(c.name, c.suggested_port);
                    setProbe((p) => ({ ...p, [c.name]: `${r.target}: ${r.message}` }));
                  } catch (e) {
                    setProbe((p) => ({ ...p, [c.name]: e instanceof Error ? e.message : "не вышло" }));
                  } finally {
                    setBusy("");
                  }
                }}
                className="rounded-md border border-line px-2 py-1 text-xs text-muted hover:border-accent hover:text-text disabled:opacity-40"
              >
                проверить {c.name}:{c.suggested_port}
              </button>
            )}
          </div>

          {probe[c.name] && <div className="mt-2 text-xs text-text">{probe[c.name]}</div>}
        </div>
      ))}
    </div>
  );
}

function Badge({ state }: { state: Container["reachability"] }) {
  const map: Record<Container["reachability"], [string, string]> = {
    ready: ["готов", "text-ok border-ok/40"],
    needs_edge: ["не в сети edge", "text-warn border-warn/40"],
    host_network: ["вне docker-сетей", "text-warn border-warn/40"],
    not_running: ["не запущен", "text-muted border-line"],
  };
  const [label, cls] = map[state];
  return <span className={"rounded-md border px-2 py-0.5 text-xs " + cls}>{label}</span>;
}
