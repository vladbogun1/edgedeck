import { AnimatePresence, motion } from "framer-motion";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  CornerDownRight,
  FileCode2,
  Globe2,
  Inbox,
  Layers,
  Pencil,
  Plus,
  ShieldAlert,
  ShieldCheck,
  Trash2,
  X,
  type LucideIcon,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  api,
  type ApplyResult,
  type ManualFile,
  type Overview,
  type PublicSite,
  type RouteView,
  type SeenDomain,
} from "../api";
import {
  CountUp,
  Empty,
  PageHeader,
  Section,
  SiteIcon,
  Skeleton,
  SkeletonRows,
  cn,
  hueOf,
  plural,
  rise,
  stackBy,
  stagger,
  subLabel,
  type Stack,
} from "../ui";
import { Wizard } from "./Wizard";

// Экран «Домены»: что требует внимания, что настроено, что пришло и не настроено.
// Отсюда же открывается визард — единственный способ что-то поменять.
export function Dashboard({ overview, onChanged }: { overview: Overview | null; onChanged: () => void }) {
  const [routes, setRoutes] = useState<RouteView[] | null>(null);
  const [manual, setManual] = useState<ManualFile[]>([]);
  const [live, setLive] = useState<Map<string, PublicSite>>(new Map());
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
    // Иконки и «живой ли сайт» — из той же проверки, что у витрины.
    api
      .publicSites()
      .then((p) => setLive(new Map(p.sites.map((s) => [s.domain, s]))))
      .catch(() => {});
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

  const certDays = useMemo(() => {
    const m = new Map<string, number>();
    for (const c of overview?.certificates ?? []) for (const d of c.domains) m.set(d, c.days_left);
    return m;
  }, [overview]);

  if (!overview || routes === null) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-12 w-64" />
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-28" />
          ))}
        </div>
        <SkeletonRows rows={5} />
      </div>
    );
  }

  const own = overview.seen_domains.filter((d) => !d.foreign);
  const foreign = groupByZone(overview.seen_domains.filter((d) => d.foreign));
  const realCerts = overview.certificates.filter((c) => !c.self_signed);
  const minDays = realCerts.length ? Math.min(...realCerts.map((c) => c.days_left)) : null;
  const handFiles = manual.filter((m) => !m.is_default);

  return (
    <div className="space-y-9">
      <PageHeader
        title="Домены"
        subtitle={
          <>
            {routes.length + handFiles.length} {plural(routes.length + handFiles.length, "сайт", "сайта", "сайтов")} на{" "}
            <span className="font-mono text-text">{overview.server.public_ip || overview.server.hostname}</span>
          </>
        }
        actions={
          <button onClick={() => setWizard({})} className="btn btn-primary">
            <Plus className="size-4" strokeWidth={2.5} />
            Добавить домен
          </button>
        }
      />

      <motion.div variants={stagger} initial="hidden" animate="show" className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Kpi icon={Globe2} label="Маршрутов" value={routes.length} hint={`+ ${handFiles.length} вручную`} />
        <Kpi
          icon={ShieldCheck}
          label="Сертификатов"
          value={realCerts.length}
          hint={minDays === null ? "пока нет" : `ближайший через ${minDays} дн.`}
          tone={minDays !== null && minDays <= 21 ? "warn" : undefined}
        />
        <Kpi icon={Inbox} label="Ждут настройки" value={own.length} hint="пришли без маршрута" tone={own.length ? "accent" : undefined} />
        <Kpi
          icon={ShieldAlert}
          label="Чужих зон"
          value={foreign.length}
          hint="заблокированы, 421"
          tone={foreign.some((z) => z.pointsHere) ? "warn" : undefined}
        />
      </motion.div>

      <AnimatePresence>
        {result && (
          <motion.div
            initial={{ opacity: 0, y: -6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            className={cn("callout flex items-start gap-3", result.rolled_back ? "[--tone:var(--color-warn)]" : "[--tone:var(--color-ok)]")}
          >
            {result.rolled_back ? (
              <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warn" />
            ) : (
              <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-ok" />
            )}
            <div className="flex-1">
              <div className={result.rolled_back ? "text-warn" : "text-ok"}>{result.message}</div>
              {result.snapshot && <div className="mt-1 font-mono text-xs text-muted">снимок {result.snapshot}</div>}
            </div>
            <button onClick={() => setResult(null)} aria-label="Скрыть" className="text-muted hover:text-text">
              <X className="size-4" />
            </button>
          </motion.div>
        )}
      </AnimatePresence>

      {error && (
        <div role="alert" className="callout text-bad [--tone:var(--color-bad)]">
          {error}
        </div>
      )}

      {overview.warnings.length > 0 && (
        <div className="space-y-2">
          {overview.warnings.map((w, i) => (
            <div
              key={i}
              className={cn("callout flex gap-3", w.level === "red" ? "[--tone:var(--color-bad)]" : "[--tone:var(--color-warn)]")}
            >
              <AlertTriangle className={cn("mt-0.5 size-4 shrink-0", w.level === "red" ? "text-bad" : "text-warn")} />
              <div>
                <div className={cn("font-medium", w.level === "red" ? "text-bad" : "text-warn")}>{w.title}</div>
                <div className="mt-1 text-muted">{w.detail}</div>
                {w.action && <div className="mt-1 text-text">→ {w.action}</div>}
              </div>
            </div>
          ))}
        </div>
      )}

      <Section title="Маршруты" count={routes.length}>
        {routes.length === 0 ? (
          <Empty icon={<Globe2 className="size-5" />} title="Панель пока ничем не управляет">
            Направьте A-запись на <span className="font-mono text-text">{overview.server.public_ip || "адрес сервера"}</span> и
            нажмите «Добавить домен» — или примите под управление файл, заведённый раньше руками.
          </Empty>
        ) : (
          <motion.div variants={stagger} initial="hidden" animate="show" className="space-y-4">
            {stackBy(routes, (r) => r.zone, (r) => r.domain).map((st) => (
              <motion.div key={st.zone} variants={rise}>
                <RouteStack
                  stack={st}
                  live={live}
                  certDays={certDays}
                  busy={busy}
                  onEdit={(r) => setWizard({ editing: r })}
                  onDelete={async (r) => {
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
                />
              </motion.div>
            ))}
          </motion.div>
        )}
      </Section>

      {handFiles.length > 0 && (
        <Section
          title="Написаны руками"
          count={handFiles.length}
          hint="Эти файлы панель не перезаписывает. «Принять» — разобрать файл в маршрут и дальше вести из панели; содержимое станет сгенерированным."
        >
          <div className="card rows overflow-hidden">
            {handFiles.map((m) => {
              const domain = m.addresses.find((a) => a.includes(".") && !/^https?:\/\/$/.test(a))?.replace(/^https?:\/\//, "") ?? m.file;
              const site = live.get(domain);
              return (
                <div key={m.file} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3.5 sm:px-5">
                  <SiteIcon domain={domain} src={site?.icon} size={34} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-medium">{domain}</span>
                      {site && <LiveChip online={site.online} />}
                    </div>
                    <div className="mt-0.5 flex items-center gap-1.5 truncate text-xs text-muted">
                      <FileCode2 className="size-3.5 shrink-0" />
                      <span className="font-mono">{m.file}</span>
                      {m.targets.length > 0 && (
                        <>
                          <ArrowRight className="size-3 shrink-0" />
                          <span className="truncate font-mono">{m.targets.join(", ")}</span>
                        </>
                      )}
                    </div>
                  </div>
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
                      className="btn btn-soft btn-sm"
                    >
                      Принять под управление
                    </button>
                  ) : (
                    <span className="max-w-sm text-right text-xs text-faint">{m.why_not}</span>
                  )}
                </div>
              );
            })}
          </div>
        </Section>
      )}

      <Section title="Пришли, но не настроены" count={own.length}>
        {own.length === 0 ? (
          <div className="card flex items-center gap-3 px-5 py-4 text-sm text-muted">
            <CheckCircle2 className="size-4 text-ok" />
            Ничего лишнего: все домены, которые дошли до сервера, уже настроены.
          </div>
        ) : (
          <div className="card rows overflow-hidden">
            {own.map((d) => (
              <div key={d.host} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3.5 sm:px-5">
                <SiteIcon domain={d.host} size={34} />
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium">{d.host}</div>
                  <div className="mt-0.5 text-xs text-muted">
                    {d.hits} {plural(d.hits, "заход", "захода", "заходов")} · {ago(d.last_seen)}
                  </div>
                </div>
                {d.tls_asked && (
                  <span className={cn("chip", d.tls_allowed ? "chip-ok" : "chip-warn")}>
                    {d.tls_allowed ? "сертификат разрешён" : "смотрит не сюда"}
                  </span>
                )}
                <div className="flex gap-2">
                  <button onClick={() => setWizard({ domain: d.host })} className="btn btn-soft btn-sm">
                    Настроить
                  </button>
                  <button
                    onClick={async () => {
                      await api.forgetDomain(d.host);
                      onChanged();
                    }}
                    className="btn btn-ghost btn-sm"
                  >
                    Забыть
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </Section>

      {foreign.length > 0 && (
        <Section
          title="Чужие домены"
          count={foreign.length}
          hint="Зоны, которых нет среди ваших сайтов: чужие записи на этот IP и сканеры с подставленным именем. Им отдаётся пустой 421 — ни формы входа, ни списка сайтов, ни сертификата."
        >
          <div className="card rows overflow-hidden">
            {foreign.map((z) => (
              <div key={z.zone} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3.5 sm:px-5">
                <span
                  className={cn(
                    "grid size-[34px] shrink-0 place-items-center rounded-[10px] border",
                    z.pointsHere ? "border-warn/30 bg-warn/10 text-warn" : "border-line bg-white/[0.03] text-faint",
                  )}
                >
                  <ShieldAlert className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate font-mono text-[13.5px]">{z.zone}</div>
                  <div className="mt-0.5 text-xs text-muted">
                    {z.items.length} {plural(z.items.length, "имя", "имени", "имён")} · {z.hits}{" "}
                    {plural(z.hits, "заход", "захода", "заходов")} · {ago(z.last)}
                  </div>
                </div>
                <span className={cn("chip", z.pointsHere ? "chip-warn" : "")}>
                  {z.pointsHere ? "A-запись смотрит сюда" : "сканер"}
                </span>
                <div className="flex gap-2">
                  <button
                    onClick={async () => {
                      if (!confirm(`Зона ${z.zone} ваша? Панель начнёт ей отвечать и выпускать сертификаты.`)) return;
                      await api.trustZone(z.zone, true);
                      onChanged();
                    }}
                    className="btn btn-ghost btn-sm"
                  >
                    Это мой домен
                  </button>
                  <button
                    onClick={async () => {
                      for (const d of z.items) await api.forgetDomain(d.host);
                      onChanged();
                    }}
                    aria-label="Забыть"
                    className="btn btn-ghost btn-sm btn-icon !w-[30px]"
                  >
                    <X className="size-3.5" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        </Section>
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

function Kpi({
  icon: Icon,
  label,
  value,
  hint,
  tone,
}: {
  icon: LucideIcon;
  label: string;
  value: number;
  hint: string;
  tone?: "warn" | "accent";
}) {
  return (
    <motion.div variants={rise} className="card relative overflow-hidden p-4 sm:p-5">
      {tone && (
        <span
          className={cn(
            "absolute inset-x-0 top-0 h-[2px]",
            tone === "warn" ? "bg-warn shadow-[0_0_12px_var(--color-warn)]" : "bg-[image:var(--accent-grad)] shadow-[var(--glow-accent)]",
          )}
        />
      )}
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-muted">{label}</span>
        <span
          className={cn(
            "grid size-8 place-items-center rounded-lg",
            tone === "warn" ? "bg-warn/12 text-warn" : "bg-accent/12 text-accent-hi",
          )}
        >
          <Icon className="size-4" />
        </span>
      </div>
      <div className="kpi-num mt-3 text-[30px]">
        <CountUp value={value} />
      </div>
      <div className="mt-1.5 truncate text-xs text-faint">{hint}</div>
    </motion.div>
  );
}

// Стек маршрутов одной зоны: шапка проекта и его домены; пути одного домена
// (/logs/, /vault/) — вложенными строками под ним.
function RouteStack({
  stack,
  live,
  certDays,
  busy,
  onEdit,
  onDelete,
}: {
  stack: Stack<RouteView>;
  live: Map<string, PublicSite>;
  certDays: Map<string, number>;
  busy: string;
  onEdit: (r: RouteView) => void;
  onDelete: (r: RouteView) => void;
}) {
  const hue = hueOf(stack.zone);
  // Домены по порядку стека, у каждого — корень и пути.
  const byDomain = new Map<string, RouteView[]>();
  for (const r of stack.members) byDomain.set(r.domain, [...(byDomain.get(r.domain) ?? []), r]);
  const domains = [...byDomain.entries()].map(([domain, list]) => {
    const sorted = [...list].sort((a, b) => (a.path === "/" ? -1 : b.path === "/" ? 1 : (a.path ?? "").localeCompare(b.path ?? "")));
    return { domain, head: sorted[0], paths: sorted.slice(1) };
  });
  const sites = domains.map((d) => live.get(d.domain)).filter(Boolean) as PublicSite[];
  const up = sites.filter((s) => s.online).length;
  const multi = domains.length > 1;

  return (
    <div className="card relative overflow-hidden">
      {multi && (
        <>
          <span
            className="absolute inset-y-0 left-0 w-[3px]"
            style={{ background: `linear-gradient(to bottom, hsl(${hue} 85% 62%), hsl(${(hue + 50) % 360} 85% 50% / .3))` }}
          />
          <div className="flex items-center gap-3 border-b border-line bg-white/[0.015] px-4 py-3 sm:px-5">
            <span
              className="grid size-8 place-items-center rounded-lg border border-white/10"
              style={{ background: `linear-gradient(135deg, hsl(${hue} 80% 55% / .35), hsl(${(hue + 50) % 360} 80% 45% / .15))` }}
            >
              <Layers className="size-4 text-white" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="truncate font-display font-semibold">{stack.zone}</div>
              <div className="text-xs text-faint">
                стек · {domains.length} {plural(domains.length, "домен", "домена", "доменов")}
              </div>
            </div>
            {sites.length > 0 && (
              <span className={cn("chip", up === sites.length ? "chip-ok" : "chip-warn")}>
                <span className={cn("dot !size-1.5", up === sites.length && "dot-live")} />
                {up === sites.length ? "все работают" : `${up} из ${sites.length}`}
              </span>
            )}
          </div>
        </>
      )}
      <div className={cn("rows", multi && "pl-[3px]")}>
        {domains.map((d) => (
          <div key={d.domain}>
            <RouteRow
              r={d.head}
              label={multi ? subLabel(d.domain, stack.zone) : undefined}
              hue={hue}
              site={live.get(d.domain)}
              days={certDays.get(d.domain)}
              busy={busy === d.head.id}
              onEdit={() => onEdit(d.head)}
              onDelete={() => onDelete(d.head)}
            />
            {d.paths.map((r) => (
              <PathRow key={r.id} r={r} busy={busy === r.id} onEdit={() => onEdit(r)} onDelete={() => onDelete(r)} />
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

function PathRow({ r, busy, onEdit, onDelete }: { r: RouteView; busy: boolean; onEdit: () => void; onDelete: () => void }) {
  return (
    <div className="group flex flex-wrap items-center gap-x-3 gap-y-1 py-2 pr-4 pl-[66px] text-sm transition-colors hover:bg-white/[0.02] sm:pr-5 sm:pl-[72px]">
      <CornerDownRight className="size-3.5 shrink-0 text-faint" />
      <span className="font-mono text-[13px] text-text">{r.path}</span>
      <ArrowRight className="size-3 shrink-0 text-faint" />
      <span className="min-w-0 truncate font-mono text-xs text-muted">{r.target_addr || r.type}</span>
      {r.auth?.upstream && <span className="chip chip-accent">вход</span>}
      {r.max_upload && <span className="chip font-mono">{r.max_upload}</span>}
      <div className="ml-auto flex gap-1.5 opacity-100 transition-opacity lg:opacity-0 lg:group-focus-within:opacity-100 lg:group-hover:opacity-100">
        <button onClick={onEdit} aria-label={`Изменить ${r.id}`} className="btn btn-ghost btn-sm btn-icon !size-7">
          <Pencil className="size-3.5" />
        </button>
        <button
          disabled={busy}
          onClick={onDelete}
          aria-label={`Удалить ${r.id}`}
          className="btn btn-ghost btn-sm btn-icon !size-7 hover:!border-bad/40 hover:!text-bad"
        >
          <Trash2 className="size-3.5" />
        </button>
      </div>
    </div>
  );
}

function RouteRow({
  r,
  label,
  hue,
  site,
  days,
  busy,
  onEdit,
  onDelete,
}: {
  r: RouteView;
  label?: string;
  hue?: number;
  site?: PublicSite;
  days?: number;
  busy: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const target =
    r.type === "proxy"
      ? r.target_addr
      : r.type === "static"
        ? "статика"
        : r.type === "redirect"
          ? r.redirect_to
          : r.type === "acme_only"
            ? "только сертификат"
            : "заглушка";
  return (
    <div className="group flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3.5 transition-colors hover:bg-white/[0.02] sm:px-5">
      <SiteIcon domain={r.domain} src={site?.icon} size={36} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          {label && (
            <span
              className="shrink-0 rounded-md px-1.5 py-0.5 font-mono text-[11px] font-semibold"
              style={{ background: `hsl(${hue} 80% 60% / .15)`, color: `hsl(${hue} 90% 80%)` }}
            >
              {label}
            </span>
          )}
          <a
            href={`https://${r.domain}${r.path === "/" ? "" : r.path}`}
            className="truncate font-medium transition-colors hover:text-accent-hi"
          >
            {r.domain}
            {r.path !== "/" && <span className="text-muted">{r.path}</span>}
          </a>
          {site && <LiveChip online={site.online} />}
          {r.hsts && <span className="chip chip-accent">HSTS</span>}
          {r.auth?.upstream && <span className="chip chip-accent">вход</span>}
          {(r.headers?.length ?? 0) > 0 && <span className="chip">свои заголовки</span>}
          {r.compress && <span className="chip">сжатие</span>}
          {r.acme_webroot && <span className="chip">certbot webroot</span>}
        </div>
        <div className="mt-0.5 flex items-center gap-1.5 text-xs text-muted">
          <ArrowRight className="size-3 shrink-0" />
          <span className="truncate font-mono">{target}</span>
          {(r.aliases?.length ?? 0) > 0 && <span className="truncate text-faint">· также {r.aliases!.join(", ")}</span>}
        </div>
      </div>
      {typeof days === "number" && (
        <span className={cn("chip font-mono", days <= 7 ? "chip-bad" : days <= 21 ? "chip-warn" : "")}>
          <ShieldCheck className="size-3" />
          {days} дн.
        </span>
      )}
      <div className="flex gap-1.5 opacity-100 transition-opacity lg:opacity-0 lg:group-focus-within:opacity-100 lg:group-hover:opacity-100">
        <button onClick={onEdit} aria-label={`Изменить ${r.domain}`} className="btn btn-ghost btn-sm btn-icon !w-[30px]">
          <Pencil className="size-3.5" />
        </button>
        <button
          disabled={busy}
          onClick={onDelete}
          aria-label={`Удалить ${r.domain}`}
          className="btn btn-ghost btn-sm btn-icon !w-[30px] hover:!border-bad/40 hover:!text-bad"
        >
          <Trash2 className="size-3.5" />
        </button>
      </div>
    </div>
  );
}

function LiveChip({ online }: { online: boolean }) {
  return (
    <span className={cn("chip !h-5 !px-2 !text-[10.5px]", online ? "chip-ok" : "chip-bad")}>
      <span className={cn("dot !size-1.5", online && "dot-live")} />
      {online ? "работает" : "лежит"}
    </span>
  );
}

function ago(iso: string) {
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 90) return "только что";
  if (s < 3600) return `${Math.round(s / 60)} мин назад`;
  if (s < 86400) return `${Math.round(s / 3600)} ч назад`;
  return `${Math.round(s / 86400)} дн назад`;
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
