import { motion } from "framer-motion";
import { AlertTriangle, ArrowUpRight, CheckCircle2, Layers, ServerCrash, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { api, type FleetServer, type PublicSite } from "../api";
import { Meter, PageHeader, Section, SiteIcon, Skeleton, cn, hueOf, plural, rise, stackBy, stagger, subLabel } from "../ui";

// «Флот»: все серверы пользователя на одном экране — только чтение. Управлять
// сервером — на его собственной панели (кнопка «Открыть панель»).
export function FleetView() {
  const [servers, setServers] = useState<FleetServer[] | null>(null);
  const [error, setError] = useState("");

  async function load() {
    try {
      const f = await api.fleet();
      setServers(f.servers);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "не загрузилось");
    }
  }

  useEffect(() => {
    void load();
    const t = setInterval(() => void load(), 30000);
    return () => clearInterval(t);
  }, []);

  if (error) {
    return (
      <div role="alert" className="callout text-bad [--tone:var(--color-bad)]">
        {error}
      </div>
    );
  }
  if (!servers) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-12 w-64" />
        <div className="grid gap-3 md:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-48" />
          ))}
        </div>
      </div>
    );
  }

  const up = servers.filter((s) => s.ok);
  // Всё, что требует внимания, — с меткой сервера, красное первым.
  const attention = up
    .flatMap((s) =>
      (s.summary!.warnings ?? []).map((w) => ({ server: s.member.id, level: w.level, title: w.title })),
    )
    .concat(
      up
        .filter((s) => s.summary!.drift)
        .map((s) => ({ server: s.member.id, level: "yellow" as const, title: "routes.yaml разошёлся с применённым" })),
    )
    .sort((a, b) => Number(b.level === "red") - Number(a.level === "red"));
  const down = servers.filter((s) => !s.ok);

  // Сайты всех серверов с меткой, где живут, — и стеки поверх них.
  const all: (PublicSite & { server: string })[] = up.flatMap((s) =>
    (s.summary!.sites ?? []).filter((x) => x.kind !== "редирект").map((x) => ({ ...x, server: s.member.id })),
  );
  const stacks = stackBy(all, (x) => x.zone, (x) => x.domain);

  const certs = up
    .flatMap((s) => (s.summary!.certificates ?? []).map((c) => ({ ...c, server: s.member.id })))
    .sort((a, b) => a.days_left - b.days_left)
    .slice(0, 6);

  return (
    <div className="space-y-9">
      <PageHeader
        title="Флот"
        subtitle={
          <>
            {servers.length} {plural(servers.length, "сервер", "сервера", "серверов")} · {all.length}{" "}
            {plural(all.length, "сервис", "сервиса", "сервисов")} · {stacks.length}{" "}
            {plural(stacks.length, "проект", "проекта", "проектов")}
          </>
        }
      />

      <motion.div variants={stagger} initial="hidden" animate="show" className="grid gap-3 md:grid-cols-3">
        {servers.map((s) => (
          <motion.div key={s.member.id} variants={rise}>
            <ServerCard s={s} />
          </motion.div>
        ))}
      </motion.div>

      <div className="grid gap-6 lg:grid-cols-[1.4fr_1fr]">
        <Section title="Стеки на всех серверах" count={stacks.length}>
          <div className="card rows overflow-hidden">
            {stacks.map((st) => {
              const hue = hueOf(st.zone);
              const where = [...new Set(st.members.map((m) => m.server))];
              const ok = st.members.every((m) => m.online);
              return (
                <div key={st.zone} className="flex items-center gap-3 px-4 py-3">
                  <SiteIcon domain={st.main.domain} src={st.main.icon} size={32} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-medium">{st.zone}</span>
                      {st.members.length > 1 && <Layers className="size-3.5 shrink-0 text-faint" />}
                    </div>
                    <div className="mt-1 flex flex-wrap gap-1">
                      {st.members.map((m) => (
                        <span
                          key={m.domain}
                          className="rounded-md px-1.5 py-0.5 font-mono text-[10.5px]"
                          style={{ background: `hsl(${hue} 80% 60% / .13)`, color: `hsl(${hue} 90% 80%)` }}
                          title={m.domain}
                        >
                          {subLabel(m.domain, st.zone)}
                        </span>
                      ))}
                    </div>
                  </div>
                  <div className="flex gap-1">
                    {where.map((w) => (
                      <span key={w} className="chip font-mono !text-[10.5px]">
                        {w}
                      </span>
                    ))}
                  </div>
                  <span className={cn("dot !size-2", ok ? "text-ok" : "text-bad")} />
                </div>
              );
            })}
          </div>
        </Section>

        <div className="space-y-6">
          <Section title="Требует внимания" count={attention.length + down.length}>
            {attention.length + down.length === 0 ? (
              <div className="card flex items-center gap-3 px-4 py-3 text-sm text-muted">
                <CheckCircle2 className="size-4 text-ok" />
                На всех серверах спокойно.
              </div>
            ) : (
              <div className="card rows overflow-hidden">
                {down.map((s) => (
                  <div key={"down" + s.member.id} className="flex items-start gap-3 px-4 py-2.5 text-sm">
                    <ServerCrash className="mt-0.5 size-4 shrink-0 text-bad" />
                    <span className="chip font-mono">{s.member.id}</span>
                    <span className="text-bad">{s.error}</span>
                  </div>
                ))}
                {attention.map((a, i) => (
                  <div key={i} className="flex items-start gap-3 px-4 py-2.5 text-sm">
                    <AlertTriangle className={cn("mt-0.5 size-4 shrink-0", a.level === "red" ? "text-bad" : "text-warn")} />
                    <span className="chip shrink-0 font-mono">{a.server}</span>
                    <span className="min-w-0">{a.title}</span>
                  </div>
                ))}
              </div>
            )}
          </Section>

          <Section title="Ближайшие сертификаты">
            <div className="card rows overflow-hidden">
              {certs.map((c) => (
                <div key={c.server + c.domains[0]} className="flex items-center gap-3 px-4 py-2.5 text-sm">
                  <ShieldCheck className={cn("size-4 shrink-0", c.days_left <= 7 ? "text-bad" : c.days_left <= 21 ? "text-warn" : "text-ok")} />
                  <span className="min-w-0 flex-1 truncate">{c.domains[0]}</span>
                  <span className="chip font-mono !text-[10.5px]">{c.server}</span>
                  <span className="w-12 text-right font-mono text-xs text-muted">{c.days_left} дн.</span>
                </div>
              ))}
            </div>
          </Section>
        </div>
      </div>
    </div>
  );
}

function ServerCard({ s }: { s: FleetServer }) {
  const m = s.member;
  if (!s.ok || !s.summary) {
    return (
      <div className="card flex h-full flex-col p-4">
        <Head s={s} />
        <div className="callout mt-3 text-sm text-bad [--tone:var(--color-bad)]">{s.error}</div>
      </div>
    );
  }
  const sv = s.summary.server;
  const sites = (s.summary.sites ?? []).filter((x) => x.kind !== "редирект");
  const projects = new Set(sites.map((x) => x.zone || x.domain)).size;
  const warnings = s.summary.warnings?.length ?? 0;
  return (
    <div className={cn("card flex h-full flex-col p-4", m.self && "border-accent/40")}>
      <Head s={s} />
      <div className="mt-1 text-xs text-muted">
        {sites.length} {plural(sites.length, "сервис", "сервиса", "сервисов")} · {projects}{" "}
        {plural(projects, "проект", "проекта", "проектов")}
      </div>
      <div className="mt-4 space-y-2.5">
        <Row label="CPU" value={`${sv.load_avg.toFixed(2)} / ${sv.cpu_count}`}>
          <Meter value={sv.cpu_count ? sv.load_avg / sv.cpu_count : 0} max={1} />
        </Row>
        <Row label="RAM" value={`${(sv.mem_used_mb / 1024).toFixed(1)} / ${(sv.mem_total_mb / 1024).toFixed(0)} ГБ`}>
          <Meter value={sv.mem_used_mb} max={sv.mem_total_mb} />
        </Row>
        <Row label="Диск" value={`${sv.disk_used_gb} / ${sv.disk_total_gb} ГБ`}>
          <Meter value={sv.disk_used_gb} max={sv.disk_total_gb} />
        </Row>
      </div>
      <div className="mt-auto flex items-center gap-2 pt-4">
        {warnings > 0 ? (
          <span className="chip chip-warn">
            {warnings} {plural(warnings, "предупреждение", "предупреждения", "предупреждений")}
          </span>
        ) : (
          <span className="chip chip-ok">всё спокойно</span>
        )}
        <span className="ml-auto font-mono text-[10.5px] text-faint">{sv.version}</span>
      </div>
    </div>
  );
}

function Head({ s }: { s: FleetServer }) {
  const m = s.member;
  return (
    <div className="flex items-center gap-2">
      <span className={cn("dot", s.ok ? "dot-live text-ok" : "text-bad")} />
      <span className="font-display font-semibold">{m.id}</span>
      <span className="truncate text-sm text-muted">{m.label}</span>
      {m.self ? (
        <span className="chip chip-accent ml-auto">эта панель</span>
      ) : (
        <a href={m.url} className="btn btn-ghost btn-sm ml-auto">
          Открыть
          <ArrowUpRight className="size-3.5" />
        </a>
      )}
    </div>
  );
}

function Row({ label, value, children }: { label: string; value: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1 flex justify-between text-[11px]">
        <span className="text-muted">{label}</span>
        <span className="font-mono text-faint">{value}</span>
      </div>
      {children}
    </div>
  );
}
