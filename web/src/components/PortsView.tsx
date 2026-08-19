import { useEffect, useState } from "react";
import { api, type HostPort } from "../api";

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
  const [onlyOpen, setOnlyOpen] = useState(false);

  useEffect(() => {
    void (async () => setData(await api.ports()))();
  }, []);

  if (!data) return <div className="text-muted">загружаю…</div>;

  if (!data.available) {
    return (
      <div className="rounded-xl border border-warn/40 bg-warn/5 p-4 text-sm">
        <div className="font-medium text-warn">Порты хоста не видны</div>
        <div className="mt-1 text-muted">{data.error}</div>
        <div className="mt-1 text-muted">
          → в docker-compose панели должно быть <code>/proc:/host/proc:ro</code>
        </div>
      </div>
    );
  }

  const open = data.ports.filter((p) => p.verdict?.level === "open").length;
  const shown = onlyOpen ? data.ports.filter((p) => p.verdict?.level === "open") : data.ports;

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <span className={open > 0 ? "text-warn" : "text-ok"}>
          {open > 0 ? `видно из интернета: ${open}` : "лишнего наружу не смотрит"}
        </span>
        <label className="flex items-center gap-2 text-xs text-muted">
          <input type="checkbox" checked={onlyOpen} onChange={(e) => setOnlyOpen(e.target.checked)} />
          показать только открытые
        </label>
        <span className="ml-auto text-xs text-muted">
          {data.firewall_ok ? "правила фаервола: " : "⚠ "}
          {data.firewall_status}
        </span>
      </div>

      <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
        {shown.map((p) => (
          <details key={`${p.port}/${p.proto}`} className="group">
            <summary className="flex cursor-pointer flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-2.5">
              <span className="w-20 font-mono text-sm">
                {p.port}/{p.proto}
              </span>
              <span className="text-sm">
                {p.container || p.process || (p.guess ? `похоже на ${p.guess}` : "процесс на хосте")}
              </span>
              {p.container && <span className="text-xs text-muted">контейнер</span>}
              <span className={"ml-auto text-xs " + levelClass(p.verdict?.level)}>
                {p.verdict?.summary ?? "?"}
              </span>
            </summary>
            <div className="space-y-2 border-t border-line px-4 py-3 text-xs text-muted">
              <div>{p.verdict?.detail}</div>
              <div>
                слушает {p.bind_ip === "::" ? "все адреса" : p.bind_ip}
                {p.pid > 0 && !p.is_docker && <> · pid {p.pid}</>}
              </div>
              {p.verdict?.advice && (
                <div>
                  <div className="mb-1 text-text">что сделать:</div>
                  <pre className="overflow-x-auto rounded-md bg-ink px-2 py-1 text-xs whitespace-pre-wrap">
                    {p.verdict.advice}
                  </pre>
                </div>
              )}
            </div>
          </details>
        ))}
      </div>

      <p className="text-xs text-muted">
        Порты, опубликованные докером, <b>не</b> закрываются ufw: трафик идёт через DNAT в цепочку FORWARD, а ufw
        фильтрует INPUT. Ограничить такой порт можно только правилом в DOCKER-USER. Панель фаервол не меняет — только
        читает и подсказывает команду. Security List в консоли Oracle она не видит вовсе.
      </p>
      <p className="text-xs text-muted">
        Имя процесса вне докера панель не показывает намеренно: для этого контейнеру нужны <code>pid: host</code> и{" "}
        <code>CAP_SYS_PTRACE</code>, а таких прав она себе не берёт.
      </p>
    </div>
  );
}

function levelClass(level?: string) {
  switch (level) {
    case "open":
      return "text-warn";
    case "limited":
      return "text-accent";
    case "closed":
      return "text-ok";
    default:
      return "text-muted";
  }
}
