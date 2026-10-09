import { useEffect, useRef, useState } from "react";
import { api, type PublicSite } from "../api";

// Главная страница до входа: витрина сайтов этого сервера и вход в управление.
//
// Наружу тут только то, что публично и так: имена доменов и то, что сайты сами
// о себе публикуют (title, description, иконка, og:image). Ни контейнеров, ни
// портов, ни ненастроенных доменов, ни имени сервера — см. internal/server/public.go.
export function Login({ onSuccess }: { onSuccess: () => void }) {
  const [sites, setSites] = useState<PublicSite[] | null>(null);
  const [showLogin, setShowLogin] = useState(false);

  useEffect(() => {
    void (async () => {
      try {
        setSites((await api.publicSites()).sites);
      } catch {
        setSites([]);
      }
    })();
  }, []);

  const online = sites?.filter((s) => s.online).length ?? 0;

  return (
    <div className="mx-auto flex min-h-full w-full max-w-5xl flex-col gap-8 px-4 py-10 sm:py-14">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="text-2xl font-semibold tracking-tight">EdgeDeck</div>
          <div className="mt-1 text-sm text-muted">
            {sites === null
              ? "сайты этого сервера"
              : sites.length === 0
                ? "на этом сервере пока нет сайтов"
                : `${sites.length} ${plural(sites.length, "сайт", "сайта", "сайтов")} · ${
                    online === sites.length ? "все работают" : `работают ${online} из ${sites.length}`
                  }`}
          </div>
        </div>
        {!showLogin && (
          <button
            onClick={() => setShowLogin(true)}
            className="rounded-lg border border-line px-4 py-2 text-sm text-muted hover:border-accent hover:text-text"
          >
            Управление
          </button>
        )}
      </header>

      {showLogin && <LoginPanel onSuccess={onSuccess} onCancel={() => setShowLogin(false)} />}

      <SiteGrid sites={sites} />
    </div>
  );
}

function SiteGrid({ sites }: { sites: PublicSite[] | null }) {
  if (sites === null) {
    return (
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {[0, 1, 2].map((i) => (
          <div key={i} className="h-64 animate-pulse rounded-2xl border border-line bg-panel" />
        ))}
      </div>
    );
  }
  if (sites.length === 0) {
    return (
      <div className="rounded-2xl border border-line bg-panel p-8 text-center text-sm text-muted">
        Пока ни одного сайта.
      </div>
    );
  }
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {sites.map((s) => (
        <SiteCard key={s.domain} site={s} />
      ))}
    </div>
  );
}

function SiteCard({ site }: { site: PublicSite }) {
  const [imageOk, setImageOk] = useState(Boolean(site.image));
  const hue = hueOf(site.domain);
  return (
    <a
      href={`https://${site.domain}/`}
      className="group flex flex-col overflow-hidden rounded-2xl border border-line bg-panel transition-colors hover:border-accent/60"
    >
      <div
        className="relative grid h-36 place-items-center overflow-hidden border-b border-line"
        style={{
          background: `radial-gradient(120% 120% at 0% 0%, hsl(${hue} 70% 32% / .55), transparent 60%), radial-gradient(120% 120% at 100% 100%, hsl(${(hue + 60) % 360} 70% 28% / .45), transparent 60%), var(--color-panel-2)`,
        }}
      >
        {imageOk && site.image ? (
          <img
            src={site.image}
            alt=""
            loading="lazy"
            referrerPolicy="no-referrer"
            onError={() => setImageOk(false)}
            className="size-full object-cover opacity-90 transition-opacity group-hover:opacity-100"
          />
        ) : (
          <SiteIcon site={site} size="large" />
        )}
        <StatusChip online={site.online} />
      </div>
      <div className="flex flex-1 flex-col gap-1 p-4">
        <div className="flex items-center gap-2">
          {imageOk && site.image && <SiteIcon site={site} size="small" />}
          <span className="truncate font-medium">{site.title || site.domain}</span>
        </div>
        {site.title && site.title !== site.domain && <div className="truncate text-xs text-muted">{site.domain}</div>}
        {site.description && <p className="mt-1 line-clamp-2 text-sm text-muted">{site.description}</p>}
        <div className="mt-auto flex items-center justify-between pt-3 text-xs text-muted">
          <span>{site.kind}</span>
          <span className="transition-colors group-hover:text-accent">открыть →</span>
        </div>
      </div>
    </a>
  );
}

// Иконка сайта; если её нет или не загрузилась — первая буква домена.
function SiteIcon({ site, size }: { site: PublicSite; size: "large" | "small" }) {
  const [ok, setOk] = useState(Boolean(site.icon));
  const box = size === "large" ? "size-16 rounded-2xl text-2xl" : "size-5 rounded-md text-[10px]";
  if (ok && site.icon) {
    return (
      <img
        src={site.icon}
        alt=""
        loading="lazy"
        referrerPolicy="no-referrer"
        onError={() => setOk(false)}
        className={`${box} shrink-0 bg-ink/40 object-contain ${size === "large" ? "p-2 shadow-lg" : ""}`}
      />
    );
  }
  return (
    <span className={`${box} grid shrink-0 place-items-center bg-ink/50 font-semibold uppercase text-text`}>
      {site.domain.replace(/^www\./, "")[0]}
    </span>
  );
}

function StatusChip({ online }: { online: boolean }) {
  return (
    <span className="absolute top-3 right-3 flex items-center gap-1.5 rounded-full bg-ink/70 px-2.5 py-1 text-xs backdrop-blur">
      <span className={"size-1.5 rounded-full " + (online ? "bg-ok" : "bg-bad")} />
      {online ? "работает" : "не отвечает"}
    </span>
  );
}

// ---- вход ----

function LoginPanel({ onSuccess, onCancel }: { onSuccess: () => void; onCancel: () => void }) {
  const [telegram, setTelegram] = useState<boolean | null>(null);
  const [mode, setMode] = useState<"code" | "password">("code");

  useEffect(() => {
    void api
      .loginMethods()
      .then((m) => {
        setTelegram(m.telegram);
        if (!m.telegram) setMode("password");
      })
      .catch(() => {
        setTelegram(false);
        setMode("password");
      });
  }, []);

  return (
    <section className="mx-auto w-full max-w-sm rounded-2xl border border-line bg-panel p-5">
      {telegram === null ? (
        <div className="text-sm text-muted">…</div>
      ) : mode === "code" ? (
        <CodeForm onSuccess={onSuccess} />
      ) : (
        <PasswordForm onSuccess={onSuccess} />
      )}
      <div className="mt-4 flex justify-between text-xs text-muted">
        {telegram ? (
          <button onClick={() => setMode(mode === "code" ? "password" : "code")} className="underline hover:text-text">
            {mode === "code" ? "войти паролем" : "код в Telegram"}
          </button>
        ) : (
          <span />
        )}
        <button onClick={onCancel} className="underline hover:text-text">
          закрыть
        </button>
      </div>
    </section>
  );
}

function CodeForm({ onSuccess }: { onSuccess: () => void }) {
  const [challenge, setChallenge] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const input = useRef<HTMLInputElement>(null);

  async function send() {
    setBusy(true);
    setError("");
    try {
      setChallenge((await api.sendCode()).challenge);
      setCode("");
      setTimeout(() => input.current?.focus(), 0);
    } catch (err) {
      setError(err instanceof Error ? err.message : "не вышло");
    } finally {
      setBusy(false);
    }
  }

  async function submit(value: string) {
    setBusy(true);
    setError("");
    try {
      await api.loginCode(challenge, value);
      onSuccess();
    } catch (err) {
      setError(err instanceof Error ? err.message : "не вышло");
      setCode("");
    } finally {
      setBusy(false);
    }
  }

  if (!challenge) {
    return (
      <div>
        <div className="mb-1 font-medium">Вход</div>
        <p className="mb-4 text-sm text-muted">Пришлю одноразовый код в ваш Telegram.</p>
        {error && <div className="mb-3 rounded-lg bg-bad/10 px-3 py-2 text-sm text-bad">{error}</div>}
        <button
          onClick={send}
          disabled={busy}
          className="w-full rounded-lg bg-accent px-3 py-2 font-medium text-ink disabled:opacity-40"
        >
          {busy ? "отправляю…" : "Получить код в Telegram"}
        </button>
      </div>
    );
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void submit(code);
      }}
    >
      <label htmlFor="otp" className="mb-1 block font-medium">
        Код из Telegram
      </label>
      <p className="mb-3 text-sm text-muted">Действует 5 минут.</p>
      <input
        id="otp"
        ref={input}
        autoFocus
        inputMode="numeric"
        autoComplete="one-time-code"
        maxLength={6}
        value={code}
        onChange={(e) => {
          const v = e.target.value.replace(/\D/g, "").slice(0, 6);
          setCode(v);
          if (v.length === 6 && !busy) void submit(v);
        }}
        className="mb-3 w-full rounded-lg border border-line bg-ink px-3 py-2 text-center font-mono text-2xl tracking-[0.5em] outline-none focus:border-accent"
      />
      {error && <div className="mb-3 rounded-lg bg-bad/10 px-3 py-2 text-sm text-bad">{error}</div>}
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={busy || code.length !== 6}
          className="flex-1 rounded-lg bg-accent px-3 py-2 font-medium text-ink disabled:opacity-40"
        >
          {busy ? "проверяю…" : "Войти"}
        </button>
        <button
          type="button"
          onClick={send}
          disabled={busy}
          className="rounded-lg border border-line px-3 py-2 text-sm text-muted hover:text-text disabled:opacity-40"
        >
          ещё код
        </button>
      </div>
    </form>
  );
}

function PasswordForm({ onSuccess }: { onSuccess: () => void }) {
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.login(password);
      onSuccess();
    } catch (err) {
      setError(err instanceof Error ? err.message : "не вышло");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit}>
      <label className="mb-2 block font-medium" htmlFor="pw">
        Пароль
      </label>
      <input
        id="pw"
        type="password"
        autoFocus
        autoComplete="current-password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        className="mb-3 w-full rounded-lg border border-line bg-ink px-3 py-2 outline-none focus:border-accent"
      />
      {error && <div className="mb-3 rounded-lg bg-bad/10 px-3 py-2 text-sm text-bad">{error}</div>}
      <button
        type="submit"
        disabled={busy || password.length === 0}
        className="w-full rounded-lg bg-accent px-3 py-2 font-medium text-ink disabled:opacity-40"
      >
        {busy ? "проверяю…" : "Войти"}
      </button>
    </form>
  );
}

// Цвет фона карточки — из имени домена, чтобы у каждого сайта был свой и
// не менялся от загрузки к загрузке.
function hueOf(s: string) {
  let h = 0;
  for (const ch of s) h = (h * 31 + ch.charCodeAt(0)) % 360;
  return h;
}

function plural(n: number, one: string, few: string, many: string) {
  const mod10 = n % 10;
  const mod100 = n % 100;
  if (mod10 === 1 && mod100 !== 11) return one;
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return few;
  return many;
}
