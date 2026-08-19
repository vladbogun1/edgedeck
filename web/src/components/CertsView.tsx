import type { Certificate } from "../api";

// Сертификатами управляет Caddy, панель — витрина и сторож: раньше на сервере 1
// продление молча не работало месяцами (DESIGN.md §2.7).
export function CertsView({ certs }: { certs: Certificate[] }) {
  if (certs.length === 0) {
    return (
      <div className="rounded-xl border border-line bg-panel p-4 text-sm text-muted">
        Сертификатов пока нет. Они появятся сами, когда домен будет смотреть на этот сервер и его откроют в браузере.
      </div>
    );
  }
  return (
    <div className="space-y-3">
      <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
        {certs.map((c) => (
          <div key={c.domains.join(",")} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-3">
            <span className="font-medium">{c.domains.join(", ")}</span>
            <span className="text-xs text-muted">{c.issuer}</span>
            {c.self_signed && (
              <span className="rounded-md border border-line px-2 py-0.5 text-xs text-muted">
                самоподписанный — так и должно быть при заходе по IP
              </span>
            )}
            <span
              className={
                "ml-auto text-sm " + (c.days_left <= 7 ? "text-bad" : c.days_left <= 21 ? "text-warn" : "text-muted")
              }
            >
              до {new Date(c.not_after).toLocaleDateString("ru-RU")} ({c.days_left} дн.)
            </span>
          </div>
        ))}
      </div>
      <p className="text-xs text-muted">
        Caddy продлевает за 30 дней до конца. Если счётчик ушёл ниже 21 — что-то мешает продлению, чаще всего домен
        перестал смотреть на этот сервер. Том <code>caddy_data</code> удалять нельзя: у Let’s Encrypt лимит на
        повторный выпуск — 5 в неделю на набор имён.
      </p>
    </div>
  );
}
