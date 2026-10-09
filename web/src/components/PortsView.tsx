import { motion } from "framer-motion";
import { ChevronDown, Globe, Lock, Network, ShieldAlert, ShieldCheck, ShieldHalf } from "lucide-react";
import { useEffect, useState } from "react";
import { api, type HostPort } from "../api";
import { CountUp, PageHeader, Segmented, SkeletonRows, cn } from "../ui";

// Экран портов отвечает на вопрос, который на старых серверах не имел быстрого
// ответа: «а это видно из интернета?» (DESIGN.md §8).
//
// Вывод собирается из трёх слоёв: кто слушает → есть ли правило в DOCKER-USER
// (для docker-портов) → есть ли правило ufw и доходит ли до него трафик вообще.
export function PortsView() {
  const [data, setData] = useState<{
    available: boolean;
    error?: string;
    ports: HostPort[];
    firewall_ok?: boolean;
    firewall_status?: string;
  } | null>(null);
  const [filter, setFilter] = useState<"all" | "open" | "limited" | "closed">("all");

  useEffect(() => {
    void (async () => setData(await api.ports()))();
  }, []);

  if (!data) {
    return (
      <div className="space-y-6">
        <PageHeader title="Порты" subtitle="загружаю…" />
        <SkeletonRows rows={8} />
      </div>
    );
  }

  if (!data.available) {
    return (
      <div className="space-y-6">
        <PageHeader title="Порты" />
        <div className="callout [--tone:var(--color-warn)]">
          <div className="font-medium text-warn">Порты хоста не видны</div>
          <div className="mt-1 text-muted">{data.error}</div>
          <div className="mt-1 text-muted">
            → в docker-compose панели должно быть <code>/proc:/host/proc:ro</code>
          </div>
        </div>
      </div>
    );
  }

  const count = (l: string) => data.ports.filter((p) => p.verdict?.level === l).length;
  const open = count("open");
  const shown = filter === "all" ? data.ports : data.ports.filter((p) => p.verdict?.level === filter);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Порты"
        subtitle="Кто слушает на сервере и видно ли это из интернета"
        actions={
          <Segmented
            value={filter}
            onChange={setFilter}
            options={[
              { id: "all", label: "Все", n: data.ports.length },
              { id: "open", label: "Открыты", n: open },
              { id: "limited", label: "Ограничены", n: count("limited") },
              { id: "closed", label: "Закрыты", n: count("closed") },
            ]}
          />
        }
      />

      <div className="grid gap-3 sm:grid-cols-3">
        <Summary
          icon={open > 0 ? ShieldAlert : ShieldCheck}
          tone={open > 0 ? "warn" : "ok"}
          value={open}
          label={open > 0 ? "видно из интернета" : "лишнего наружу не смотрит"}
        />
        <Summary icon={Network} value={data.ports.length} label="портов слушают" />
        <div className="card flex items-center gap-3 p-4">
          <span
            className={cn(
              "grid size-9 shrink-0 place-items-center rounded-lg",
              data.firewall_ok ? "bg-ok/10 text-ok" : "bg-warn/10 text-warn",
            )}
          >
            <ShieldHalf className="size-4.5" />
          </span>
          <div className="min-w-0">
            <div className="text-sm font-medium">Фаервол</div>
            <div className="truncate text-xs text-muted">{data.firewall_status}</div>
          </div>
        </div>
      </div>

      <div className="card rows overflow-hidden">
        {shown.map((p) => (
          <details key={`${p.port}/${p.proto}`} className="group">
            <summary className="flex list-none items-center gap-x-4 gap-y-1 px-4 py-3 transition-colors hover:bg-white/[0.02] sm:px-5">
              <span className="w-24 shrink-0 font-mono text-[13.5px] font-medium">
                {p.port}
                <span className="text-faint">/{p.proto}</span>
              </span>
              <span className="flex min-w-0 flex-1 items-center gap-2 text-sm">
                {p.container ? <span className="chip !h-5 !text-[10.5px]">docker</span> : null}
                <span className="truncate">
                  {p.container || p.process || (p.guess ? `похоже на ${p.guess}` : "процесс на хосте")}
                </span>
              </span>
              <Verdict level={p.verdict?.level} text={p.verdict?.summary ?? "?"} />
              <ChevronDown className="size-4 shrink-0 text-faint transition-transform group-open:rotate-180" />
            </summary>
            <motion.div
              initial={{ opacity: 0, y: -4 }}
              animate={{ opacity: 1, y: 0 }}
              className="space-y-2 border-t border-line bg-black/20 px-4 py-3.5 text-xs text-muted sm:px-5"
            >
              <div className="text-sm text-text">{p.verdict?.detail}</div>
              <div>
                слушает <span className="font-mono">{p.bind_ip === "::" ? "все адреса" : p.bind_ip}</span>
                {p.pid > 0 && !p.is_docker && <> · pid {p.pid}</>}
              </div>
              {p.verdict?.advice && (
                <div>
                  <div className="mb-1 text-text">что сделать:</div>
                  <pre className="thin-scroll overflow-x-auto rounded-lg border border-line bg-black/30 p-3 font-mono whitespace-pre-wrap">
                    {p.verdict.advice}
                  </pre>
                </div>
              )}
            </motion.div>
          </details>
        ))}
      </div>

      <div className="space-y-2 text-xs leading-relaxed text-faint">
        <p>
          Порты, опубликованные докером, <b className="text-muted">не</b> закрываются ufw: трафик идёт через DNAT в цепочку
          FORWARD, а ufw фильтрует INPUT. Ограничить такой порт можно только правилом в DOCKER-USER. Панель фаервол не
          меняет — только читает и подсказывает команду. Security List в консоли Oracle она не видит вовсе.
        </p>
        <p>
          Имя процесса вне докера панель не показывает намеренно: для этого контейнеру нужны <code>pid: host</code> и{" "}
          <code>CAP_SYS_PTRACE</code>, а таких прав она себе не берёт.
        </p>
      </div>
    </div>
  );
}

function Summary({
  icon: Icon,
  value,
  label,
  tone,
}: {
  icon: typeof Network;
  value: number;
  label: string;
  tone?: "ok" | "warn";
}) {
  return (
    <div className="card flex items-center gap-3 p-4">
      <span
        className={cn(
          "grid size-9 shrink-0 place-items-center rounded-lg",
          tone === "warn" ? "bg-warn/10 text-warn" : tone === "ok" ? "bg-ok/10 text-ok" : "bg-accent/12 text-accent-hi",
        )}
      >
        <Icon className="size-4.5" />
      </span>
      <div>
        <div className={cn("kpi-num text-2xl", tone === "warn" && "text-warn")}>
          <CountUp value={value} />
        </div>
        <div className="text-xs text-muted">{label}</div>
      </div>
    </div>
  );
}

function Verdict({ level, text }: { level?: string; text: string }) {
  const map: Record<string, [string, typeof Globe]> = {
    open: ["chip-warn", Globe],
    limited: ["chip-accent", ShieldHalf],
    closed: ["chip-ok", Lock],
  };
  const [cls, Icon] = map[level ?? ""] ?? ["", Network];
  return (
    <span className={cn("chip hidden max-w-[45%] sm:inline-flex", cls)}>
      <Icon className="size-3 shrink-0" />
      <span className="truncate">{text}</span>
    </span>
  );
}
