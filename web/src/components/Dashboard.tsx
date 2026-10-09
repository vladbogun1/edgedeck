import { useCallback, useEffect, useState } from "react";
import { api, type ApplyResult, type ManualFile, type Overview, type RouteView, type SeenDomain } from "../api";
import { Wizard } from "./Wizard";

// Экран «Домены»: что требует внимания, что настроено, что пришло и не настроено.
// Отсюда же открывается визард — единственный способ что-то поменять.
export function Dashboard({ overview, onChanged }: { overview: Overview | null; onChanged: () => void }) {
  const [routes, setRoutes] = useState<RouteView[]>([]);
  const [manual, setManual] = useState<ManualFile[]>([]);
  const [wizard, setWizard] = useState<{ domain?: string; editing?: RouteView } | null>(null);
  const [result, setResult] = useState<ApplyResult | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const r = await api.routes();
      setRoutes(r.routes ?? []);
      setManual(r.manual ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "маршруты не загрузились");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function afterChange(res: ApplyResult) {
    setResult(res);
    setWizard(null);
    await load();
    onChanged();
  }

  if (!overview) return <div className="text-muted">загружаю…</div>;

  const certDays = new Map<string, number>();
  for (const c of overview.certificates) for (const d of c.domains) certDays.set(d, c.days_left);

  const own = overview.seen_domains.filter((d) => !d.foreign);
  const foreign = groupByZone(overview.seen_domains.filter((d) => d.foreign));

  return (
    <div className="space-y-6">
      {result && (
        <div
          className={
            "rounded-xl border px-4 py-3 text-sm " +
            (result.rolled_back ? "border-warn/40 bg-warn/5" : "border-ok/40 bg-ok/5")
          }
        >
          <div className={result.rolled_back ? "text-warn" : "text-ok"}>{result.message}</div>
          {result.snapshot && <div className="mt-1 text-xs text-muted">снимок: {result.snapshot}</div>}
          <button onClick={() => setResult(null)} className="mt-1 text-xs text-muted underline">
            скрыть
          </button>
        </div>
      )}

      {error && <div className="rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm text-bad">{error}</div>}

      {overview.warnings.length > 0 && (
        <section className="space-y-2">
          {overview.warnings.map((w, i) => (
            <div
              key={i}
              className={
                "rounded-xl border px-4 py-3 text-sm " +
                (w.level === "red" ? "border-bad/40 bg-bad/5" : "border-warn/40 bg-warn/5")
              }
            >
              <div className={"font-medium " + (w.level === "red" ? "text-bad" : "text-warn")}>{w.title}</div>
              <div className="mt-1 text-muted">{w.detail}</div>
              {w.action && <div className="mt-1 text-muted">→ {w.action}</div>}
            </div>
          ))}
        </section>
      )}

      <section>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-medium tracking-wide text-muted uppercase">Маршруты</h2>
          <button
            onClick={() => setWizard({})}
            className="rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-ink"
          >
            + Добавить домен
          </button>
        </div>

        {routes.length === 0 ? (
          <div className="rounded-xl border border-line bg-panel p-6 text-sm text-muted">
            Панель пока ничем не управляет. Направьте A-запись на{" "}
            <span className="text-text">{overview.server.public_ip || "адрес сервера"}</span> и нажмите «Добавить
            домен» — или примите под управление файл, заведённый раньше руками (ниже).
          </div>
        ) : (
          <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
            {routes.map((r) => {
              const days = certDays.get(r.domain);
              return (
                <div key={r.id} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-3">
                  <a href={`https://${r.domain}${r.path === "/" ? "" : r.path}`} className="font-medium hover:text-accent">
                    {r.domain}
                    {r.path !== "/" && <span className="text-muted">{r.path}</span>}
                  </a>
                  {(r.aliases?.length ?? 0) > 0 && <span className="text-xs text-muted">+ {r.aliases!.join(", ")}</span>}
                  <span className="text-sm text-muted">
                    {r.type === "proxy"
                      ? "→ " + r.target_addr
                      : r.type === "static"
                        ? "→ статика"
                        : r.type === "redirect"
                          ? "→ " + r.redirect_to
                          : r.type === "acme_only"
                            ? "только сертификат"
                            : "заглушка"}
                  </span>
                  {r.hsts && <span className="text-xs text-warn">HSTS</span>}
                  <span className="ml-auto flex items-center gap-3 text-xs text-muted">
                    {typeof days === "number" && (
                      <span className={days <= 7 ? "text-bad" : days <= 21 ? "text-warn" : ""}>{days} дн.</span>
                    )}
                    <button onClick={() => setWizard({ editing: r })} className="underline hover:text-text">
                      изменить
                    </button>
                    <button
                      disabled={busy === r.id}
                      onClick={async () => {
                        if (!confirm(`Удалить маршрут ${r.id}? Файл сайта тоже удалится.`)) return;
                        setBusy(r.id);
                        try {
                          await afterChange(await api.routeDelete(r.id));
                        } catch (e) {
                          setError(e instanceof Error ? e.message : "не вышло");
                        } finally {
                          setBusy("");
                        }
                      }}
                      className="underline hover:text-bad"
                    >
                      удалить
                    </button>
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </section>

      {manual.length > 0 && (
        <section>
          <h2 className="mb-3 text-sm font-medium tracking-wide text-muted uppercase">Файлы, написанные руками</h2>
          <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
            {manual.map((m) => (
              <div key={m.file} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-3">
                <span className="font-mono text-sm">{m.file}</span>
                <span className="text-xs text-muted">{m.addresses.join(", ")}</span>
                {m.targets.length > 0 && <span className="text-xs text-muted">→ {m.targets.join(", ")}</span>}
                {m.can_import ? (
                  <button
                    disabled={busy === m.file}
                    onClick={async () => {
                      setBusy(m.file);
                      try {
                        await afterChange(await api.routeImport(m.file));
                      } catch (e) {
                        setError(e instanceof Error ? e.message : "не вышло");
                      } finally {
                        setBusy("");
                      }
                    }}
                    className="ml-auto rounded-md border border-accent px-2 py-1 text-xs text-accent disabled:opacity-40"
                  >
                    принять под управление
                  </button>
                ) : (
                  <span className="ml-auto max-w-md text-right text-xs text-muted">{m.why_not}</span>
                )}
              </div>
            ))}
          </div>
          <p className="mt-2 text-xs text-muted">
            Эти файлы панель не перезаписывает. «Принять под управление» — разобрать файл в маршрут и дальше вести его
            из панели; содержимое при этом станет сгенерированным.
          </p>
        </section>
      )}

      <section>
        <h2 className="mb-3 text-sm font-medium tracking-wide text-muted uppercase">Пришли, но не настроены</h2>
        {own.length === 0 ? (
          <div className="rounded-xl border border-line bg-panel p-4 text-sm text-muted">
            Ничего лишнего: все домены, которые дошли до сервера, уже настроены.
          </div>
        ) : (
          <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
            {own.map((d) => (
              <div key={d.host} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-3">
                <span className="font-medium">{d.host}</span>
                <span className="text-xs text-muted">
                  {d.hits} {plural(d.hits, "заход", "захода", "заходов")} · последний{" "}
                  {new Date(d.last_seen).toLocaleString("ru-RU")}
                </span>
                {d.tls_asked && (
                  <span className={"text-xs " + (d.tls_allowed ? "text-ok" : "text-warn")}>
                    {d.tls_allowed ? "сертификат разрешён" : "сертификат не выпускаем: домен смотрит не сюда"}
                  </span>
                )}
                <span className="ml-auto flex gap-3 text-xs">
                  <button onClick={() => setWizard({ domain: d.host })} className="text-accent underline">
                    настроить
                  </button>
                  <button
                    onClick={async () => {
                      await api.forgetDomain(d.host);
                      onChanged();
                    }}
                    className="text-muted underline hover:text-text"
                  >
                    забыть
                  </button>
                </span>
              </div>
            ))}
          </div>
        )}
      </section>

      {foreign.length > 0 && (
        <section>
          <h2 className="mb-3 text-sm font-medium tracking-wide text-muted uppercase">Чужие домены</h2>
          <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
            {foreign.map((z) => (
              <div key={z.zone} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-3">
                <span className="font-medium">{z.zone}</span>
                <span className="text-xs text-muted">
                  {z.items.length} {plural(z.items.length, "имя", "имени", "имён")} · {z.hits}{" "}
                  {plural(z.hits, "заход", "захода", "заходов")} · последний {new Date(z.last).toLocaleString("ru-RU")}
                </span>
                <span className={"text-xs " + (z.pointsHere ? "text-warn" : "text-muted")}>
                  {z.pointsHere ? "A-запись смотрит сюда — " : ""}панель не отвечает, сертификат не выпускается
                </span>
                <span className="ml-auto flex gap-3 text-xs">
                  <button
                    onClick={async () => {
                      if (!confirm(`Зона ${z.zone} ваша? Панель начнёт ей отвечать и выпускать сертификаты.`)) return;
                      await api.trustZone(z.zone, true);
                      onChanged();
                    }}
                    className="text-accent underline"
                  >
                    это мой домен
                  </button>
                  <button
                    onClick={async () => {
                      for (const d of z.items) await api.forgetDomain(d.host);
                      onChanged();
                    }}
                    className="text-muted underline hover:text-text"
                  >
                    забыть
                  </button>
                </span>
              </div>
            ))}
          </div>
          <p className="mt-2 text-xs text-muted">
            Домены, чьей зоны нет среди ваших сайтов: чужие записи на этот IP и сканеры с подставленным именем. Им
            отдаётся пустой ответ 421 — ни формы входа, ни списка сайтов. Поддомены ваших зон сюда не попадают.
          </p>
        </section>
      )}

      {wizard && (
        <Wizard
          initialDomain={wizard.domain}
          editing={wizard.editing}
          onClose={() => setWizard(null)}
          onDone={afterChange}
        />
      )}
    </div>
  );
}

function plural(n: number, one: string, few: string, many: string) {
  const mod10 = n % 10;
  const mod100 = n % 100;
  if (mod10 === 1 && mod100 !== 11) return one;
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return few;
  return many;
}

// groupByZone сворачивает чужие домены по зоне: wildcard-запись даёт сотни имён
// вида www.www.mail.…, и построчно они бы утопили список. Сверху — те, чья
// A-запись действительно смотрит сюда.
function groupByZone(list: SeenDomain[]) {
  const zones = new Map<string, { zone: string; items: SeenDomain[]; hits: number; last: string; pointsHere: boolean }>();
  for (const d of list) {
    const key = d.zone || d.host;
    let z = zones.get(key);
    if (!z) {
      z = { zone: key, items: [], hits: 0, last: d.last_seen, pointsHere: false };
      zones.set(key, z);
    }
    z.items.push(d);
    z.hits += d.hits;
    if (d.last_seen > z.last) z.last = d.last_seen;
    if (d.points_here) z.pointsHere = true;
  }
  return [...zones.values()].sort((a, b) => Number(b.pointsHere) - Number(a.pointsHere) || b.hits - a.hits);
}
