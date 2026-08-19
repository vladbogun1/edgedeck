import { useEffect, useState } from "react";
import { api, type HostPort } from "../api";

// Экран портов отвечает на вопрос, который на старых серверах не имел быстрого
// ответа: «а это видно из интернета?» (DESIGN.md §8).
export function PortsView() {
  const [data, setData] = useState<{ available: boolean; error?: string; ports: HostPort[] } | null>(null);

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

  return (
    <div className="space-y-3">
      <p className="text-xs text-muted">
        Порты, опубликованные докером, <b>не</b> закрываются ufw: трафик идёт через DNAT в цепочку FORWARD, а ufw
        фильтрует INPUT. Ограничить такой порт можно только правилом в DOCKER-USER. Панель фаервол не меняет — она
        показывает картину.
      </p>
      <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel">
        {data.ports.map((p) => (
          <div key={`${p.port}/${p.proto}`} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-2.5">
            <span className="w-20 font-mono">
              {p.port}/{p.proto}
            </span>
            <span className="text-sm">{p.container || p.process || "неизвестный процесс"}</span>
            {p.is_docker && !p.container && <span className="text-xs text-muted">порт контейнера</span>}
            {p.pid > 0 && !p.is_docker && <span className="text-xs text-muted">pid {p.pid}</span>}
            <span className="ml-auto text-xs">
              {p.loopback_only ? (
                <span className="text-ok">только с самого хоста</span>
              ) : (
                <span className="text-warn">слушает на {p.bind_ip === "::" ? "всех адресах" : p.bind_ip}</span>
              )}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
