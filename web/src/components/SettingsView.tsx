import { useEffect, useState } from "react";
import { api, type Settings } from "../api";

// Настройки: состояние подсистем и две вещи, которые можно сделать —
// сменить пароль и проверить связь с Telegram.
//
// Секретов на этом экране нет: ни токена бота, ни токена агента. Панель
// показывает только «настроено / не настроено», иначе экран настроек стал бы
// удобным местом, откуда всё это утекает.
export function SettingsView() {
  const [s, setS] = useState<Settings | null>(null);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void (async () => setS(await api.settings()))();
  }, []);

  if (!s) return <div className="text-muted">загружаю…</div>;

  return (
    <div className="space-y-6">
      {note && <div className="rounded-xl border border-line bg-panel px-4 py-3 text-sm">{note}</div>}

      <section className="space-y-2">
        <h2 className="text-sm font-medium tracking-wide text-muted uppercase">Состояние</h2>
        <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel text-sm">
          <Row label="Сервер" ok value={`${s.server.hostname} · ${s.server.public_ip}`} note={s.server.edge_dir} />
          <Row label="Версия панели" ok value={s.server.version} />
          <Row label="Caddy" ok={s.caddy.alive} value={s.caddy.alive ? "admin API отвечает" : "не отвечает"} note={s.caddy.message} />
          <Row label="Доступ к Docker" ok={s.agent.ok} value={s.agent.ok ? "агент отвечает" : "агент недоступен"} note={s.agent.message} />
          <Row
            label="Правила фаервола"
            ok={s.firewall.ok}
            value={s.firewall.ok ? "читаются" : "не читаются"}
            note={s.firewall.status}
          />
          <Row
            label="Уведомления"
            ok={s.telegram.configured}
            value={s.telegram.configured ? "Telegram настроен" : "не настроены"}
            note={
              s.telegram.configured
                ? `сторож проверяет сайты раз в ${s.watch.interval}; сообщает, если молчат дольше ${s.watch.down_warn} или сертификату осталось меньше ${s.watch.cert_warn_days} дн.`
                : "задайте EDGEDECK_TELEGRAM_TOKEN и EDGEDECK_TELEGRAM_CHAT в ~/edgedeck/.env и перезапустите панель"
            }
          />
        </div>
        {s.telegram.configured && (
          <button
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                setNote((await api.testTelegram()).message);
              } catch (e) {
                setNote(e instanceof Error ? e.message : "не вышло");
              } finally {
                setBusy(false);
              }
            }}
            className="rounded-lg border border-accent px-3 py-1.5 text-sm text-accent disabled:opacity-40"
          >
            Проверить связь с Telegram
          </button>
        )}
        {!s.firewall.ufw_works && s.firewall.ok && (
          <div className="rounded-xl border border-warn/40 bg-warn/5 px-4 py-3 text-sm">
            <div className="text-warn">На этом сервере правила ufw не работают</div>
            <div className="mt-1 text-muted">
              В цепочке INPUT безусловный REJECT стоит выше перехода в ufw, поэтому любое <code>ufw allow</code>, кроме
              уже разрешённого выше, ничего не меняет. Сайты живы потому, что 80 и 443 — docker-порты и идут мимо INPUT.
              Практический вывод: всё поднимаем в докере, а хостовому сервису нужна ручная правка порядка правил.
            </div>
          </div>
        )}
      </section>

      <PasswordSection onNote={setNote} />

      {s.sessions && s.sessions.length > 0 && (
        <section>
          <h2 className="mb-2 text-sm font-medium tracking-wide text-muted uppercase">Активные сессии</h2>
          <div className="divide-y divide-line overflow-hidden rounded-xl border border-line bg-panel text-sm">
            {s.sessions.map((x) => (
              <div key={x.token} className="flex flex-wrap items-baseline gap-x-3 px-4 py-2">
                <span className="font-mono text-xs">{x.token}</span>
                <span className="text-xs text-muted">{x.ip}</span>
                <span className="ml-auto text-xs text-muted">
                  вход {new Date(x.created).toLocaleString("ru-RU")}
                </span>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

function Row({ label, ok, value, note }: { label: string; ok: boolean; value: string; note?: string }) {
  return (
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-3">
      <span className="w-40 text-muted">{label}</span>
      <span className={ok ? "text-text" : "text-warn"}>{value}</span>
      {note && <span className="ml-auto max-w-lg text-right text-xs text-muted">{note}</span>}
    </div>
  );
}

function PasswordSection({ onNote }: { onNote: (s: string) => void }) {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);

  return (
    <section className="space-y-2">
      <h2 className="text-sm font-medium tracking-wide text-muted uppercase">Пароль</h2>
      <form
        className="flex flex-wrap items-end gap-2 rounded-xl border border-line bg-panel p-4"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          try {
            await api.changePassword(current, next);
            setCurrent("");
            setNext("");
            onNote("пароль изменён — старые сессии продолжают работать, при желании выйдите везде");
          } catch (err) {
            onNote(err instanceof Error ? err.message : "не вышло");
          } finally {
            setBusy(false);
          }
        }}
      >
        <label className="text-sm">
          <span className="mb-1 block text-muted">текущий</span>
          <input
            type="password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            className="w-44 rounded-lg border border-line bg-ink px-3 py-2"
          />
        </label>
        <label className="text-sm">
          <span className="mb-1 block text-muted">новый (от 8 символов)</span>
          <input
            type="password"
            value={next}
            onChange={(e) => setNext(e.target.value)}
            className="w-44 rounded-lg border border-line bg-ink px-3 py-2"
          />
        </label>
        <button
          type="submit"
          disabled={busy || current.length === 0 || next.length < 8}
          className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-ink disabled:opacity-40"
        >
          Сменить
        </button>
      </form>
      <p className="text-xs text-muted">
        Стартовый пароль лежит в <code>~/edgedeck/.env</code>. После смены строку{" "}
        <code>EDGEDECK_INITIAL_PASSWORD</code> оттуда можно убрать — она нужна только для первого запуска.
      </p>
    </section>
  );
}
