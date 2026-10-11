import { motion } from "framer-motion";
import {
  AlertTriangle,
  Bell,
  Container,
  KeyRound,
  Loader2,
  Monitor,
  Send,
  Server,
  ShieldHalf,
  Waypoints,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useState } from "react";
import { api, type Settings } from "../api";
import { PageHeader, Section, Skeleton, cn, rise, stagger } from "../ui";

// Настройки: состояние подсистем и две вещи, которые можно сделать —
// сменить пароль и проверить связь с Telegram.
//
// Секретов на этом экране нет: ни токена бота, ни токена агента. Панель
// показывает только «настроено / не настроено», иначе экран настроек стал бы
// удобным местом, откуда всё это утекает.
export function SettingsView() {
  const [s, setS] = useState<Settings | null>(null);
  const [note, setNote] = useState<{ text: string; ok: boolean } | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void (async () => setS(await api.settings()))();
  }, []);

  if (!s) {
    return (
      <div className="space-y-6">
        <PageHeader title="Настройки" subtitle="загружаю…" />
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <Skeleton key={i} className="h-28" />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-9">
      <PageHeader
        title="Настройки"
        subtitle={
          <>
            <span className="font-mono text-text">{s.server.hostname}</span> · {s.server.public_ip} ·{" "}
            <span className="font-mono">{s.server.version}</span>
          </>
        }
      />

      {note && (
        <div className={cn("callout", note.ok ? "[--tone:var(--color-ok)]" : "text-bad [--tone:var(--color-bad)]")}>{note.text}</div>
      )}

      <Section title="Состояние">
        <motion.div variants={stagger} initial="hidden" animate="show" className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <Status icon={Server} label="Сервер" ok value={s.server.hostname} note={s.server.edge_dir} />
          <Status icon={Waypoints} label="Caddy" ok={s.caddy.alive} value={s.caddy.alive ? "admin API отвечает" : "не отвечает"} note={s.caddy.message} />
          <Status icon={Container} label="Доступ к Docker" ok={s.agent.ok} value={s.agent.ok ? "агент отвечает" : "агент недоступен"} note={s.agent.message} />
          <Status icon={ShieldHalf} label="Правила фаервола" ok={s.firewall.ok} value={s.firewall.ok ? "читаются" : "не читаются"} note={s.firewall.status} />
          <Status
            icon={Bell}
            label="Уведомления"
            ok={s.telegram.configured}
            value={s.telegram.configured ? "Telegram настроен" : "не настроены"}
            note={
              s.telegram.configured
                ? `проверка раз в ${s.watch.interval}; тревога, если сайт молчит дольше ${s.watch.down_warn} или сертификату < ${s.watch.cert_warn_days} дн.`
                : "задайте EDGEDECK_TELEGRAM_TOKEN и EDGEDECK_TELEGRAM_CHAT в ~/edgedeck/.env"
            }
            action={
              s.telegram.configured && (
                <button
                  disabled={busy}
                  onClick={async () => {
                    setBusy(true);
                    try {
                      setNote({ text: (await api.testTelegram()).message, ok: true });
                    } catch (e) {
                      setNote({ text: e instanceof Error ? e.message : "не вышло", ok: false });
                    } finally {
                      setBusy(false);
                    }
                  }}
                  className="btn btn-ghost btn-sm mt-3"
                >
                  {busy ? <Loader2 className="size-3.5 animate-spin" /> : <Send className="size-3.5" />}
                  Проверить связь
                </button>
              )
            }
          />
        </motion.div>
        {!s.firewall.ufw_works && s.firewall.ok && (
          <div className="callout mt-3 flex gap-3 [--tone:var(--color-warn)]">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warn" />
            <div>
              <div className="text-warn">На этом сервере правила ufw не работают</div>
              <div className="mt-1 text-muted">
                В цепочке INPUT безусловный REJECT стоит выше перехода в ufw, поэтому любое <code>ufw allow</code>, кроме
                уже разрешённого выше, ничего не меняет. Сайты живы потому, что 80 и 443 — docker-порты и идут мимо INPUT.
              </div>
            </div>
          </div>
        )}
      </Section>

      <PasswordSection fresh={s.password_fresh} onNote={setNote} />

      {s.sessions && s.sessions.length > 0 && (
        <Section
          title="Активные сессии"
          count={s.sessions.length}
          actions={
            <button
              onClick={async () => {
                if (!confirm("Выйти на всех устройствах и на всех серверах? Придётся войти заново.")) return;
                const r = await api.logoutAll();
                if (r.unreached?.length) alert("Не дошло до: " + r.unreached.join(", ") + " — там токены истекут сами.");
                window.location.reload();
              }}
              className="btn btn-danger btn-sm"
            >
              Выйти везде
            </button>
          }
        >
          <div className="card rows overflow-hidden">
            {s.sessions.map((x) => (
              <div key={x.token} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3 sm:px-5">
                <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-white/5 text-muted">
                  <Monitor className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm">{browserName(x.user_agent)}</div>
                  <div className="truncate font-mono text-[11px] text-faint">
                    {x.ip} · {x.token}
                  </div>
                </div>
                <span className="text-xs text-muted">вход {new Date(x.created).toLocaleString("ru-RU")}</span>
              </div>
            ))}
          </div>
        </Section>
      )}
    </div>
  );
}

function Status({
  icon: Icon,
  label,
  ok,
  value,
  note,
  action,
}: {
  icon: LucideIcon;
  label: string;
  ok: boolean;
  value: string;
  note?: string;
  action?: React.ReactNode;
}) {
  return (
    <motion.div variants={rise} className="card flex flex-col p-4">
      <div className="flex items-center gap-3">
        <span className={cn("grid size-9 shrink-0 place-items-center rounded-lg", ok ? "bg-accent/12 text-accent-hi" : "bg-warn/10 text-warn")}>
          <Icon className="size-4.5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="text-xs text-muted">{label}</div>
          <div className="flex items-center gap-2 truncate text-sm font-medium">
            <span className={cn("dot !size-1.5", ok ? "text-ok" : "text-warn")} />
            <span className="truncate">{value}</span>
          </div>
        </div>
      </div>
      {note && <p className="mt-3 line-clamp-3 text-xs leading-relaxed text-faint">{note}</p>}
      {action && <div className="mt-auto">{action}</div>}
    </motion.div>
  );
}

function PasswordSection({ fresh, onNote }: { fresh: boolean; onNote: (n: { text: string; ok: boolean }) => void }) {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);

  return (
    <Section
      title="Пароль"
      hint="Пароль — запасной вход на случай, если Telegram недоступен. Строку EDGEDECK_INITIAL_PASSWORD в ~/edgedeck/.env после смены можно убрать: она нужна только для первого запуска."
    >
      <form
        className="card flex flex-wrap items-end gap-3 p-4 sm:p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          try {
            await api.changePassword(current, next);
            setCurrent("");
            setNext("");
            onNote({ text: "Пароль изменён. Остальные сессии продолжают работать.", ok: true });
          } catch (err) {
            onNote({ text: err instanceof Error ? err.message : "не вышло", ok: false });
          } finally {
            setBusy(false);
          }
        }}
      >
        <span className="grid size-9 shrink-0 place-items-center self-center rounded-lg bg-accent/12 text-accent-hi">
          <KeyRound className="size-4.5" />
        </span>
        {!fresh && (
          <label className="min-w-44 flex-1">
            <span className="field-label">Текущий</span>
            <input type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} className="field" />
          </label>
        )}
        <label className="min-w-44 flex-1">
          <span className="field-label">Новый, от 8 символов</span>
          <input type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} className="field" />
        </label>
        <button type="submit" disabled={busy || (!fresh && current.length === 0) || next.length < 8} className="btn btn-primary h-10">
          {busy && <Loader2 className="size-4 animate-spin" />}
          {fresh ? "Задать пароль" : "Сменить"}
        </button>
        {fresh && (
          <p className="w-full text-xs text-muted">
            Вы только что вошли — текущий пароль не нужен. Можно задать новый, даже если старый забыт.
          </p>
        )}
      </form>
    </Section>
  );
}

function browserName(ua: string) {
  if (!ua) return "неизвестный браузер";
  const os = /Windows/.test(ua) ? "Windows" : /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : /Mac OS/.test(ua) ? "macOS" : /Linux/.test(ua) ? "Linux" : "";
  const br = /Edg\//.test(ua) ? "Edge" : /OPR\//.test(ua) ? "Opera" : /Chrome\//.test(ua) ? "Chrome" : /Firefox\//.test(ua) ? "Firefox" : /Safari\//.test(ua) ? "Safari" : "браузер";
  return os ? `${br} · ${os}` : br;
}
