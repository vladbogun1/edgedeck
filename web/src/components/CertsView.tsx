import { motion } from "framer-motion";
import { ShieldCheck, ShieldQuestion } from "lucide-react";
import type { Certificate } from "../api";
import { Empty, PageHeader, SkeletonRows, cn, rise, stagger } from "../ui";

// Сертификатами управляет Caddy, панель — витрина и сторож: раньше на сервере 1
// продление молча не работало месяцами (DESIGN.md §2.7).
export function CertsView({ certs }: { certs: Certificate[] | null }) {
  const real = (certs ?? []).filter((c) => !c.self_signed).sort((a, b) => a.days_left - b.days_left);
  const self = (certs ?? []).filter((c) => c.self_signed);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Сертификаты"
        subtitle={
          certs === null
            ? "загружаю…"
            : `${real.length} от Let’s Encrypt · Caddy продлевает сам за 30 дней до конца`
        }
      />

      {certs === null ? (
        <SkeletonRows rows={5} />
      ) : real.length === 0 && self.length === 0 ? (
        <Empty icon={<ShieldQuestion className="size-5" />} title="Сертификатов пока нет">
          Они появятся сами, когда домен будет смотреть на этот сервер и его откроют в браузере.
        </Empty>
      ) : (
        <motion.div variants={stagger} initial="hidden" animate="show" className="grid gap-3 md:grid-cols-2">
          {[...real, ...self].map((c) => (
            <motion.div key={c.domains.join(",")} variants={rise}>
              <CertCard c={c} />
            </motion.div>
          ))}
        </motion.div>
      )}

      <p className="text-xs leading-relaxed text-faint">
        Если счётчик ушёл ниже 21 дня — что-то мешает продлению, чаще всего домен перестал смотреть на этот сервер. Том{" "}
        <code>caddy_data</code> удалять нельзя: у Let’s Encrypt лимит на повторный выпуск — 5 в неделю на набор имён.
      </p>
    </div>
  );
}

function CertCard({ c }: { c: Certificate }) {
  const tone = c.self_signed ? "muted" : c.days_left <= 7 ? "bad" : c.days_left <= 21 ? "warn" : "ok";
  // Let's Encrypt выдаёт на 90 дней — от этого и считаем шкалу.
  const pct = Math.max(2, Math.min(100, (c.days_left / 90) * 100));
  const color = { ok: "var(--accent-grad)", warn: "var(--color-warn)", bad: "var(--color-bad)", muted: "var(--color-faint)" }[tone];
  return (
    <div className="card card-hover p-4 sm:p-5">
      <div className="flex items-start gap-3">
        <span
          className={cn(
            "grid size-9 shrink-0 place-items-center rounded-lg",
            tone === "ok" && "bg-ok/10 text-ok",
            tone === "warn" && "bg-warn/10 text-warn",
            tone === "bad" && "bg-bad/10 text-bad",
            tone === "muted" && "bg-white/5 text-faint",
          )}
        >
          <ShieldCheck className="size-4.5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium">{c.domains[0]}</div>
          <div className="truncate text-xs text-muted">
            {c.domains.length > 1 ? `+ ${c.domains.slice(1).join(", ")}` : c.issuer}
          </div>
        </div>
        {c.self_signed ? (
          <span className="chip">самоподписанный</span>
        ) : (
          <div className="text-right">
            <div
              className={cn(
                "kpi-num text-2xl",
                tone === "bad" && "text-bad",
                tone === "warn" && "text-warn",
              )}
            >
              {c.days_left}
            </div>
            <div className="text-[11px] text-faint">дн.</div>
          </div>
        )}
      </div>
      {c.self_signed ? (
        <p className="mt-3 text-xs text-faint">Так и должно быть при заходе по IP: публичных сертификатов на адреса не выдают.</p>
      ) : (
        <>
          <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-white/6">
            <motion.div
              className="h-full rounded-full"
              style={{ background: color }}
              initial={{ width: 0 }}
              animate={{ width: `${pct}%` }}
              transition={{ duration: 0.9, ease: [0.22, 1, 0.36, 1] }}
            />
          </div>
          <div className="mt-2 flex justify-between text-[11px] text-faint">
            <span>{c.issuer}</span>
            <span className="font-mono">до {new Date(c.not_after).toLocaleDateString("ru-RU")}</span>
          </div>
        </>
      )}
    </div>
  );
}
