import { motion } from "framer-motion";
import { Activity, Box, Boxes, Cable, ChevronDown, Layers, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { api, type Container } from "../api";
import { Empty, PageHeader, Segmented, SkeletonRows, cn, rise, stagger } from "../ui";

// «Цели» — то, на что вообще можно направить домен: контейнеры с их сетями и
// портами. Здесь же видно главную причину 502 на старых серверах: контейнер не
// подключён к сети прокси (DESIGN.md §2.3).
export function ContainersView() {
  const [list, setList] = useState<Container[] | null>(null);
  const [hint, setHint] = useState("");
  const [error, setError] = useState("");
  const [probe, setProbe] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const [filter, setFilter] = useState<"all" | "ready" | "attention">("all");

  async function reload() {
    try {
      const data = await api.containers();
      setList(data.containers);
      setHint(data.compose_hint);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "не загрузилось");
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  const attention = list?.filter((c) => c.reachability === "needs_edge" || c.reachability === "host_network").length ?? 0;
  const shown =
    list?.filter((c) =>
      filter === "all" ? true : filter === "ready" ? c.reachability === "ready" : c.reachability !== "ready" && c.reachability !== "not_running",
    ) ?? [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Цели"
        subtitle="Контейнеры, на которые можно направить домен"
        actions={
          list && (
            <Segmented
              value={filter}
              onChange={setFilter}
              options={[
                { id: "all", label: "Все", n: list.length },
                { id: "ready", label: "Готовы", n: list.filter((c) => c.reachability === "ready").length },
                { id: "attention", label: "Внимание", n: attention },
              ]}
            />
          )
        }
      />

      {error ? (
        <div role="alert" className="callout text-bad [--tone:var(--color-bad)]">
          {error}
        </div>
      ) : !list ? (
        <SkeletonRows rows={6} />
      ) : shown.length === 0 ? (
        <Empty icon={<Boxes className="size-5" />} title="Здесь пусто">
          Под этот фильтр контейнеров нет.
        </Empty>
      ) : (
        <motion.div variants={stagger} initial="hidden" animate="show" className="grid gap-3 lg:grid-cols-2">
          {shown.map((c) => (
            <motion.div key={c.name} variants={rise} className="card card-hover flex flex-col p-4 sm:p-5">
              <div className="flex items-start gap-3">
                <span
                  className={cn(
                    "grid size-9 shrink-0 place-items-center rounded-lg",
                    c.reachability === "ready" ? "bg-accent/12 text-accent-hi" : c.reachability === "not_running" ? "bg-white/5 text-faint" : "bg-warn/10 text-warn",
                  )}
                >
                  <Box className="size-4.5" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium">{c.name}</div>
                  <div className="truncate font-mono text-[11.5px] text-faint">{c.image}</div>
                </div>
                <Reach state={c.reachability} />
              </div>

              <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
                <span className="flex items-center gap-1.5">
                  <Cable className="size-3.5 text-faint" />
                  {c.networks.join(", ") || "без сети"}
                </span>
                {c.compose.project && (
                  <span className="flex items-center gap-1.5">
                    <Layers className="size-3.5 text-faint" />
                    {c.compose.project}
                  </span>
                )}
                <span className="flex items-center gap-1.5">
                  <Activity className="size-3.5 text-faint" />
                  {c.status}
                </span>
              </div>

              {c.ports.length > 0 && (
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {c.ports.map((p) => (
                    <span
                      key={`${p.private}/${p.type}/${p.bind_ip}`}
                      className={cn("chip font-mono", p.private === c.suggested_port && "chip-accent")}
                      title={
                        p.loopback_only
                          ? "опубликован только на 127.0.0.1: с хоста доступен, из другого контейнера — нет"
                          : p.published
                            ? `опубликован на ${p.bind_ip}:${p.public}`
                            : "виден только внутри docker-сети"
                      }
                    >
                      {p.private}/{p.type}
                      {p.published && ` → ${p.public}`}
                      {p.loopback_only && " · хост"}
                    </span>
                  ))}
                </div>
              )}

              {c.hint && <div className="callout mt-3 !py-2 text-xs text-warn [--tone:var(--color-warn)]">{c.hint}</div>}

              {c.reachability === "needs_edge" && hint && (
                <details className="group mt-3 text-xs text-muted">
                  <summary className="flex list-none items-center gap-1 hover:text-text">
                    <ChevronDown className="size-3.5 transition-transform group-open:rotate-180" />
                    как закрепить сеть навсегда
                  </summary>
                  <p className="mt-2">
                    Панель возвращает подключение сама, если контейнер пересоздали. Правильнее прописать сеть в его
                    docker-compose.yml:
                  </p>
                  <pre className="thin-scroll mt-2 overflow-x-auto rounded-lg border border-line bg-black/30 p-3 font-mono">{hint}</pre>
                </details>
              )}

              {(c.reachability === "needs_edge" || c.suggested_port > 0) && (
                <div className="mt-auto flex flex-wrap items-center gap-2 pt-4">
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
                      className="btn btn-soft btn-sm"
                    >
                      Подключить к сети edge
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
                      className="btn btn-ghost btn-sm"
                    >
                      {busy === c.name && <Loader2 className="size-3.5 animate-spin" />}
                      Проверить :{c.suggested_port}
                    </button>
                  )}
                </div>
              )}

              {probe[c.name] && (
                <div className="mt-3 rounded-lg border border-line bg-black/25 px-3 py-2 font-mono text-xs text-text">{probe[c.name]}</div>
              )}
            </motion.div>
          ))}
        </motion.div>
      )}
    </div>
  );
}

function Reach({ state }: { state: Container["reachability"] }) {
  const map: Record<Container["reachability"], [string, string]> = {
    ready: ["готов", "chip-ok"],
    needs_edge: ["не в сети edge", "chip-warn"],
    host_network: ["вне docker-сетей", "chip-warn"],
    not_running: ["не запущен", ""],
  };
  const [label, cls] = map[state];
  return <span className={cn("chip", cls)}>{label}</span>;
}
