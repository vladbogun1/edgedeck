import { useEffect, useState } from "react";
import { api, type Container, type Plan, type Route, type ApplyResult } from "../api";

// Визард добавления и правки маршрута — четыре шага из DESIGN.md §9.3:
// домен → куда → проверка → применить. Каждый шаг говорит, что он выяснил,
// и почему предлагает именно это.

type Step = 1 | 2 | 3 | 4;

export function Wizard({
  initialDomain,
  editing,
  onClose,
  onDone,
}: {
  initialDomain?: string;
  editing?: Route & { id: string };
  onClose: () => void;
  onDone: (r: ApplyResult) => void;
}) {
  const [step, setStep] = useState<Step>(editing ? 2 : 1);
  const [route, setRoute] = useState<Route>(
    editing ?? {
      domain: initialDomain ?? "",
      path: "/",
      type: "proxy",
      target: { kind: "container", name: "", port: 0 },
      security_headers: true,
      hsts: false,
    },
  );
  const [domainCheck, setDomainCheck] = useState<string>("");
  const [domainOK, setDomainOK] = useState<boolean | null>(null);
  const [probe, setProbe] = useState<string>("");
  const [plan, setPlan] = useState<Plan | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const set = (patch: Partial<Route>) => setRoute((r) => ({ ...r, ...patch }));

  async function checkDomain() {
    setBusy(true);
    setError("");
    try {
      const c = await api.checkDomain(route.domain);
      setDomainCheck(c.message);
      setDomainOK(c.points_here);
      if (c.points_here) setStep(2);
    } catch (e) {
      setError(msg(e));
    } finally {
      setBusy(false);
    }
  }

  async function runProbe() {
    setBusy(true);
    setError("");
    setProbe("");
    try {
      if (route.type === "proxy" && route.target?.kind === "container") {
        const r = await api.probe(route.target.name ?? "", route.target.port ?? 0, route.domain);
        setProbe(`${r.target}: ${r.message}`);
      } else if (route.type === "proxy" && route.target?.kind === "host") {
        const r = await api.probe("host.docker.internal", route.target.port ?? 0, route.domain);
        setProbe(`${r.target}: ${r.message}`);
      } else {
        setProbe("проверять нечего: цель не контейнер");
      }
      setStep(4);
      void makePlan();
    } catch (e) {
      setError(msg(e));
    } finally {
      setBusy(false);
    }
  }

  async function makePlan() {
    try {
      setPlan(await api.routePlan(route, editing?.id ?? ""));
    } catch (e) {
      setError(msg(e));
    }
  }

  async function applyNow() {
    setBusy(true);
    setError("");
    try {
      onDone(await api.routeSave(route, editing?.id ?? ""));
    } catch (e) {
      setError(msg(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/60 p-4 py-10">
      <div className="w-full max-w-2xl rounded-2xl border border-line bg-panel p-6">
        <div className="mb-5 flex items-baseline gap-3">
          <h2 className="text-lg font-semibold">{editing ? "Маршрут " + editing.id : "Новый домен"}</h2>
          <span className="text-xs text-muted">шаг {step} из 4</span>
          <button onClick={onClose} className="ml-auto text-sm text-muted hover:text-text">
            закрыть
          </button>
        </div>

        {error && <div className="mb-4 rounded-lg bg-bad/10 px-3 py-2 text-sm text-bad">{error}</div>}

        {step === 1 && (
          <section className="space-y-3">
            <label className="block text-sm text-muted">Домен</label>
            <input
              autoFocus
              value={route.domain}
              onChange={(e) => set({ domain: e.target.value.trim().toLowerCase() })}
              placeholder="example.com"
              className="w-full rounded-lg border border-line bg-ink px-3 py-2 outline-none focus:border-accent"
            />
            {domainCheck && (
              <div className={"text-sm " + (domainOK ? "text-ok" : "text-warn")}>{domainCheck}</div>
            )}
            <div className="flex gap-2 pt-2">
              <button
                disabled={busy || route.domain.length < 4}
                onClick={checkDomain}
                className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-ink disabled:opacity-40"
              >
                {busy ? "проверяю…" : "Проверить A-запись"}
              </button>
              {domainOK === false && (
                <button
                  onClick={() => setStep(2)}
                  className="rounded-lg border border-line px-4 py-2 text-sm text-muted hover:text-text"
                  title="можно настроить заранее: сертификат выпустится, когда запись переедет"
                >
                  всё равно продолжить
                </button>
              )}
            </div>
            <p className="text-xs text-muted">
              Проверка нужна до выпуска сертификата: у Let’s Encrypt лимит 5 повторов в неделю на набор имён, и
              бесполезные попытки его тратят.
            </p>
          </section>
        )}

        {step === 2 && <TargetStep route={route} set={set} onNext={() => setStep(3)} onBack={() => setStep(1)} />}

        {step === 3 && (
          <section className="space-y-3">
            <Options route={route} set={set} />
            <div className="flex gap-2 pt-2">
              <button
                disabled={busy}
                onClick={runProbe}
                className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-ink disabled:opacity-40"
              >
                {busy ? "стучусь…" : "Проверить цель"}
              </button>
              <button onClick={() => setStep(2)} className="rounded-lg border border-line px-4 py-2 text-sm text-muted">
                назад
              </button>
            </div>
          </section>
        )}

        {step === 4 && (
          <section className="space-y-4">
            {probe && <div className="rounded-lg border border-line bg-ink px-3 py-2 text-sm">{probe}</div>}
            {!plan && <div className="text-sm text-muted">собираю план…</div>}
            {plan && (
              <>
                <div className="text-sm">
                  {plan.valid ? (
                    <span className="text-ok">конфиг проверен Caddy — синтаксис в порядке</span>
                  ) : (
                    <span className="text-bad">конфиг не прошёл проверку: {plan.error}</span>
                  )}
                </div>
                {plan.warnings?.map((w, i) => (
                  <div key={i} className="rounded-lg border border-warn/40 bg-warn/5 px-3 py-2 text-sm text-warn">
                    {w}
                  </div>
                ))}
                <div className="space-y-2">
                  {plan.changes
                    .filter((c) => c.kind !== "unchanged")
                    .map((c) => (
                      <details key={c.file} className="rounded-lg border border-line bg-ink">
                        <summary className="cursor-pointer px-3 py-2 text-sm">
                          <span
                            className={
                              c.kind === "create" ? "text-ok" : c.kind === "delete" ? "text-bad" : "text-warn"
                            }
                          >
                            {c.kind === "create" ? "создать" : c.kind === "delete" ? "удалить" : "изменить"}
                          </span>{" "}
                          <span className="font-mono text-xs">{c.file}</span>
                        </summary>
                        <pre className="max-h-64 overflow-auto border-t border-line px-3 py-2 text-xs whitespace-pre-wrap text-muted">
                          {c.after || c.before}
                        </pre>
                      </details>
                    ))}
                  {plan.changes.every((c) => c.kind === "unchanged") && (
                    <div className="text-sm text-muted">файлы не изменятся</div>
                  )}
                </div>
                <div className="flex gap-2">
                  <button
                    disabled={busy || !plan.valid}
                    onClick={applyNow}
                    className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-ink disabled:opacity-40"
                  >
                    {busy ? "применяю…" : "Применить"}
                  </button>
                  <button
                    onClick={() => setStep(3)}
                    className="rounded-lg border border-line px-4 py-2 text-sm text-muted"
                  >
                    назад
                  </button>
                </div>
                <p className="text-xs text-muted">
                  Применение идёт транзакцией: снимок → запись → перезагрузка без разрыва соединений → проверка всех
                  доменов. Если какой-то сайт станет отвечать хуже, изменение откатится само.
                </p>
              </>
            )}
          </section>
        )}
      </div>
    </div>
  );
}

function TargetStep({
  route,
  set,
  onNext,
  onBack,
}: {
  route: Route;
  set: (p: Partial<Route>) => void;
  onNext: () => void;
  onBack: () => void;
}) {
  const [containers, setContainers] = useState<Container[] | null>(null);
  const [connecting, setConnecting] = useState("");
  const [note, setNote] = useState("");

  useEffect(() => {
    void (async () => {
      try {
        setContainers((await api.containers()).containers);
      } catch {
        setContainers([]);
      }
    })();
  }, []);

  const kinds: { value: Route["type"]; label: string; hint: string }[] = [
    { value: "proxy", label: "приложение", hint: "контейнер или процесс на сервере" },
    { value: "static", label: "статика", hint: "файлы из ~/edge/www/<домен>" },
    { value: "redirect", label: "редирект", hint: "перенаправить на другой адрес" },
    { value: "stub", label: "заглушка", hint: "домен занят, сайта пока нет" },
    { value: "acme_only", label: "только сертификат", hint: "TLS живёт в другом месте" },
  ];

  return (
    <section className="space-y-4">
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
        {kinds.map((k) => (
          <button
            key={k.value}
            onClick={() => set({ type: k.value })}
            className={
              "rounded-lg border px-3 py-2 text-left " +
              (route.type === k.value ? "border-accent bg-panel-2" : "border-line hover:border-muted")
            }
          >
            <div className="text-sm">{k.label}</div>
            <div className="text-xs text-muted">{k.hint}</div>
          </button>
        ))}
      </div>

      {route.type === "proxy" && (
        <div className="space-y-2">
          <div className="text-sm text-muted">Найдено на сервере:</div>
          {containers === null && <div className="text-sm text-muted">ищу…</div>}
          <div className="max-h-64 space-y-1 overflow-y-auto">
            {containers?.map((c) => {
              const chosen = route.target?.kind === "container" && route.target.name === c.name;
              return (
                <div
                  key={c.name}
                  className={
                    "rounded-lg border px-3 py-2 " + (chosen ? "border-accent bg-panel-2" : "border-line")
                  }
                >
                  <button
                    className="w-full text-left"
                    onClick={() =>
                      set({ target: { kind: "container", name: c.name, port: c.suggested_port || 80 } })
                    }
                  >
                    <div className="flex flex-wrap items-baseline gap-2 text-sm">
                      <span className="font-medium">{c.name}</span>
                      <span className="text-xs text-muted">:{c.suggested_port || "?"}</span>
                      <span className="text-xs text-muted">{c.networks.join(", ") || "без сети"}</span>
                      {c.reachability === "ready" && <span className="text-xs text-ok">в сети edge</span>}
                      {c.reachability === "needs_edge" && <span className="text-xs text-warn">не в сети edge</span>}
                      {c.reachability === "not_running" && <span className="text-xs text-muted">не запущен</span>}
                    </div>
                  </button>
                  {chosen && c.reachability === "needs_edge" && (
                    <button
                      disabled={connecting === c.name}
                      onClick={async () => {
                        setConnecting(c.name);
                        try {
                          const r = await api.connectEdge(c.name);
                          setNote(r.message);
                          setContainers((await api.containers()).containers);
                        } catch (e) {
                          setNote(msg(e));
                        } finally {
                          setConnecting("");
                        }
                      }}
                      className="mt-2 rounded-md border border-accent px-2 py-1 text-xs text-accent disabled:opacity-40"
                    >
                      подключить к сети edge
                    </button>
                  )}
                </div>
              );
            })}
          </div>

          {route.target?.kind === "container" && (
            <div className="flex items-center gap-2 text-sm">
              <span className="text-muted">порт</span>
              <input
                type="number"
                value={route.target.port || ""}
                onChange={(e) =>
                  set({ target: { ...route.target!, port: parseInt(e.target.value || "0", 10) } })
                }
                className="w-24 rounded-lg border border-line bg-ink px-2 py-1"
              />
              <button
                onClick={() => set({ target: { kind: "host", port: route.target?.port ?? 0 } })}
                className="ml-auto text-xs text-muted underline hover:text-text"
              >
                это процесс на хосте, не контейнер
              </button>
            </div>
          )}

          {route.target?.kind === "host" && (
            <div className="space-y-2 rounded-lg border border-warn/40 bg-warn/5 p-3 text-sm">
              <div className="text-warn">Процесс на хосте</div>
              <div className="flex items-center gap-2">
                <span className="text-muted">порт</span>
                <input
                  type="number"
                  value={route.target.port || ""}
                  onChange={(e) => set({ target: { kind: "host", port: parseInt(e.target.value || "0", 10) } })}
                  className="w-24 rounded-lg border border-line bg-ink px-2 py-1"
                />
                <button
                  onClick={() => set({ target: { kind: "container", name: "", port: 0 } })}
                  className="ml-auto text-xs text-muted underline hover:text-text"
                >
                  вернуться к контейнерам
                </button>
              </div>
              <p className="text-xs text-muted">
                На этом сервере такое соединение может не пройти: стоковое правило Oracle в цепочке INPUT стоит выше
                правил ufw, поэтому хостовые порты недоступны даже из контейнеров. Проверка на следующем шаге покажет
                честно.
              </p>
            </div>
          )}
          {note && <div className="text-xs text-text">{note}</div>}
        </div>
      )}

      {route.type === "redirect" && (
        <div>
          <label className="mb-1 block text-sm text-muted">Куда перенаправлять</label>
          <input
            value={route.redirect_to ?? ""}
            onChange={(e) => set({ redirect_to: e.target.value.trim() })}
            placeholder="https://example.com"
            className="w-full rounded-lg border border-line bg-ink px-3 py-2"
          />
        </div>
      )}

      {route.type === "static" && (
        <p className="text-sm text-muted">
          Файлы читаются из <code>~/edge/www/{route.domain || "<домен>"}</code>. Каталог создастся при применении, если
          его нет — положите туда index.html.
        </p>
      )}

      <div className="flex gap-2 pt-2">
        <button
          onClick={onNext}
          disabled={route.type === "proxy" && !(route.target?.port ?? 0)}
          className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-ink disabled:opacity-40"
        >
          Дальше
        </button>
        <button onClick={onBack} className="rounded-lg border border-line px-4 py-2 text-sm text-muted">
          назад
        </button>
      </div>
    </section>
  );
}

function Options({ route, set }: { route: Route; set: (p: Partial<Route>) => void }) {
  return (
    <div className="space-y-3">
      <div className="text-sm text-muted">Дополнительно — можно оставить как есть</div>

      <label className="flex items-start gap-2 text-sm">
        <input
          type="checkbox"
          checked={route.security_headers ?? false}
          onChange={(e) => set({ security_headers: e.target.checked })}
          className="mt-1"
        />
        <span>
          Базовые заголовки безопасности <span className="text-muted">(nosniff, referrer, сжатие)</span>
        </span>
      </label>

      <label className="flex items-start gap-2 text-sm">
        <input type="checkbox" checked={route.hsts ?? false} onChange={(e) => set({ hsts: e.target.checked })} className="mt-1" />
        <span>
          HSTS
          <span className="block text-xs text-warn">
            билет в одну сторону: браузер запомнит на год, и если TLS сломается — сайт станет недоступен, откатить
            нельзя. Включайте, когда домен устоялся.
          </span>
        </span>
      </label>

      <div className="flex flex-wrap items-center gap-3 text-sm">
        <label className="flex items-center gap-2">
          <span className="text-muted">размер загрузки</span>
          <input
            value={route.max_upload ?? ""}
            onChange={(e) => set({ max_upload: e.target.value.trim() })}
            placeholder="напр. 128MB"
            className="w-28 rounded-lg border border-line bg-ink px-2 py-1"
          />
        </label>
        <label className="flex items-center gap-2">
          <span className="text-muted">путь</span>
          <input
            value={route.path ?? "/"}
            onChange={(e) => set({ path: e.target.value.trim() || "/" })}
            className="w-28 rounded-lg border border-line bg-ink px-2 py-1"
          />
        </label>
      </div>

      <label className="block text-sm">
        <span className="text-muted">дополнительные имена, через запятую</span>
        <input
          value={(route.aliases ?? []).join(", ")}
          onChange={(e) =>
            set({
              aliases: e.target.value
                .split(",")
                .map((s) => s.trim().toLowerCase())
                .filter(Boolean),
            })
          }
          placeholder="www.example.com"
          className="mt-1 w-full rounded-lg border border-line bg-ink px-3 py-2"
        />
      </label>

      <label className="block text-sm">
        <span className="text-muted">заметка для себя</span>
        <input
          value={route.comment ?? ""}
          onChange={(e) => set({ comment: e.target.value })}
          className="mt-1 w-full rounded-lg border border-line bg-ink px-3 py-2"
        />
      </label>
    </div>
  );
}

function msg(e: unknown) {
  return e instanceof Error ? e.message : "не вышло";
}
