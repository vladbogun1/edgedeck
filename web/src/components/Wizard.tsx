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
    <div className="thin-scroll fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/60 p-4 py-10 backdrop-blur-[6px]">
      <div className="pop hud w-full max-w-2xl p-6 sm:p-7">
        <div className="mb-6">
          <div className="flex items-center gap-3">
            <h2 className="page-title text-xl">{editing ? "Маршрут " + editing.id : "Новый домен"}</h2>
            <span className="rounded-full bg-white/6 px-2 py-0.5 font-mono text-[11px] text-muted">шаг {step} / 4</span>
            <button onClick={onClose} className="btn btn-ghost btn-sm ml-auto">
              закрыть
            </button>
          </div>
          <div className="mt-4 grid grid-cols-4 gap-1.5">
            {[1, 2, 3, 4].map((n) => (
              <span
                key={n}
                className={
                  "h-1 rounded-full transition-colors duration-300 " +
                  (n <= step ? "bg-[image:var(--accent-grad)] shadow-[var(--glow-accent)]" : "bg-white/8")
                }
              />
            ))}
          </div>
        </div>

        {error && <div role="alert" className="callout mb-4 text-bad [--tone:var(--color-bad)]">{error}</div>}

        {step === 1 && (
          <section className="space-y-3">
            <label className="block text-sm text-muted">Домен</label>
            <input
              autoFocus
              value={route.domain}
              onChange={(e) => set({ domain: e.target.value.trim().toLowerCase() })}
              placeholder="example.com"
              className="field"
            />
            {domainCheck && (
              <div className={"text-sm " + (domainOK ? "text-ok" : "text-warn")}>{domainCheck}</div>
            )}
            <div className="flex gap-2 pt-2">
              <button
                disabled={busy || route.domain.length < 4}
                onClick={checkDomain}
                className="btn btn-primary"
              >
                {busy ? "проверяю…" : "Проверить A-запись"}
              </button>
              {domainOK === false && (
                <button
                  onClick={() => setStep(2)}
                  className="btn btn-ghost"
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
                className="btn btn-primary"
              >
                {busy ? "стучусь…" : "Проверить цель"}
              </button>
              <button onClick={() => setStep(2)} className="btn btn-ghost">
                назад
              </button>
            </div>
          </section>
        )}

        {step === 4 && (
          <section className="space-y-4">
            {probe && <div className="rounded-xl border border-line bg-black/25 px-3 py-2 text-sm">{probe}</div>}
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
                  <div key={i} className="callout text-warn [--tone:var(--color-warn)]">
                    {w}
                  </div>
                ))}
                <div className="space-y-2">
                  {plan.changes
                    .filter((c) => c.kind !== "unchanged")
                    .map((c) => (
                      <details key={c.file} className="overflow-hidden rounded-xl border border-line bg-black/25">
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
                    className="btn btn-primary"
                  >
                    {busy ? "применяю…" : "Применить"}
                  </button>
                  <button
                    onClick={() => setStep(3)}
                    className="btn btn-ghost"
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
              "rounded-xl border px-3 py-2.5 text-left transition-all " +
              (route.type === k.value
                ? "border-accent/60 bg-accent/10 shadow-[0_0_0_3px_rgb(91_157_255/0.12)]"
                : "border-line bg-white/[0.02] hover:border-line-strong")
            }
          >
            <div className="text-sm font-medium">{k.label}</div>
            <div className="text-xs text-muted">{k.hint}</div>
          </button>
        ))}
      </div>

      {route.type === "proxy" && (
        <div className="space-y-2">
          <div className="text-sm text-muted">Найдено на сервере:</div>
          {containers === null && <div className="text-sm text-muted">ищу…</div>}
          <div className="thin-scroll max-h-64 space-y-1.5 overflow-y-auto pr-1">
            {containers?.map((c) => {
              const chosen = route.target?.kind === "container" && route.target.name === c.name;
              return (
                <div
                  key={c.name}
                  className={
                    "rounded-xl border px-3 py-2 transition-colors " + (chosen ? "border-accent/60 bg-accent/10" : "border-line bg-white/[0.02] hover:border-line-strong")
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
                      className="btn btn-soft btn-sm mt-2"
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
                className="field !h-8 w-24"
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
            <div className="callout space-y-2 [--tone:var(--color-warn)]">
              <div className="text-warn">Процесс на хосте</div>
              <div className="flex items-center gap-2">
                <span className="text-muted">порт</span>
                <input
                  type="number"
                  value={route.target.port || ""}
                  onChange={(e) => set({ target: { kind: "host", port: parseInt(e.target.value || "0", 10) } })}
                  className="field !h-8 w-24"
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
            className="field"
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
          className="btn btn-primary"
        >
          Дальше
        </button>
        <button onClick={onBack} className="btn btn-ghost">
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
          className="mt-1 accent-[var(--color-accent)]"
        />
        <span>
          Базовые заголовки безопасности <span className="text-muted">(nosniff, referrer, сжатие)</span>
        </span>
      </label>

      <label className="flex items-start gap-2 text-sm">
        <input type="checkbox" checked={route.hsts ?? false} onChange={(e) => set({ hsts: e.target.checked })} className="mt-1 accent-[var(--color-accent)]" />
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
            className="field !h-8 w-28"
          />
        </label>
        <label className="flex items-center gap-2">
          <span className="text-muted">путь</span>
          <input
            value={route.path ?? "/"}
            onChange={(e) => set({ path: e.target.value.trim() || "/" })}
            className="field !h-8 w-28"
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
          className="field mt-1"
        />
      </label>

      <label className="block text-sm">
        <span className="text-muted">заметка для себя</span>
        <input
          value={route.comment ?? ""}
          onChange={(e) => set({ comment: e.target.value })}
          className="field mt-1"
        />
      </label>
    </div>
  );
}

function msg(e: unknown) {
  return e instanceof Error ? e.message : "не вышло";
}
