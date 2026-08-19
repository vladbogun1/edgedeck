import { api, type Overview } from "../api";

// Главный экран: сначала то, что требует внимания, потом список доменов,
// потом домены, которые пришли на сервер, но маршрута ещё не имеют.
export function Dashboard({ overview, onChanged }: { overview: Overview | null; onChanged: () => void }) {
  if (!overview) return <div className="text-muted">загружаю…</div>;

  const sites = overview.sites.filter((s) => !s.is_default);
  const certByDomain = new Map<string, number>();
  for (const c of overview.certificates) {
    for (const d of c.domains) certByDomain.set(d, c.days_left);
  }

  return (
    <div className="space-y-6">
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
        <h2 className="mb-3 text-sm font-medium tracking-wide text-muted uppercase">Настроенные домены</h2>
        {sites.length === 0 ? (
          <div className="rounded-xl border border-line bg-panel p-6 text-sm text-muted">
            Доменов пока нет. Направьте A-запись на{" "}
            <span className="text-text">{overview.server.public_ip || "адрес этого сервера"}</span> и откройте домен в
            браузере — вы попадёте сюда, и он появится ниже, в списке «пришли, но не настроены».
          </div>
        ) : (
          <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
            {sites.map((s) => {
              const main = s.addresses[0] ?? s.file;
              const days = certByDomain.get(main.replace(/^https?:\/\//, ""));
              return (
                <div key={s.file} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-3">
                  <span className="font-medium">{main}</span>
                  {s.addresses.length > 1 && (
                    <span className="text-xs text-muted">+ {s.addresses.slice(1).join(", ")}</span>
                  )}
                  <span className="text-sm text-muted">→ {s.targets.join(", ") || kindName(s.kind)}</span>
                  <span className="ml-auto flex items-center gap-3 text-xs text-muted">
                    {typeof days === "number" && (
                      <span className={days <= 7 ? "text-bad" : days <= 21 ? "text-warn" : ""}>
                        сертификат {days} дн.
                      </span>
                    )}
                    {!s.managed && <span title="файл написан руками, панель его не перегенерирует">руками</span>}
                    <span className="font-mono">{s.file}</span>
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </section>

      <section>
        <h2 className="mb-3 text-sm font-medium tracking-wide text-muted uppercase">
          Пришли, но не настроены
        </h2>
        {overview.seen_domains.length === 0 ? (
          <div className="rounded-xl border border-line bg-panel p-4 text-sm text-muted">
            Ничего лишнего: все домены, которые дошли до сервера, уже настроены.
          </div>
        ) : (
          <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
            {overview.seen_domains.map((d) => (
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
                <button
                  onClick={async () => {
                    await api.forgetDomain(d.host);
                    onChanged();
                  }}
                  className="ml-auto rounded-md border border-line px-2 py-1 text-xs text-muted hover:border-accent hover:text-text"
                >
                  забыть
                </button>
              </div>
            ))}
          </div>
        )}
        <p className="mt-2 text-xs text-muted">
          Панель — обработчик по умолчанию, поэтому она видит все такие домены сама, без разбора логов.
          Настройка маршрутов появится на следующем этапе; пока сайты добавляются через <code>bin/newsite</code>.
        </p>
      </section>
    </div>
  );
}

function kindName(kind: string) {
  switch (kind) {
    case "static":
      return "статика";
    case "redirect":
      return "редирект";
    case "proxy":
      return "проксирование";
    case "mixed":
      return "смешанный";
    default:
      return "не разобрал файл";
  }
}

function plural(n: number, one: string, few: string, many: string) {
  const mod10 = n % 10;
  const mod100 = n % 100;
  if (mod10 === 1 && mod100 !== 11) return one;
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return few;
  return many;
}
