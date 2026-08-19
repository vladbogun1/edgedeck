import { useEffect, useState } from "react";
import { api, type PublicSite } from "../api";

// Главная страница до входа: витрина сайтов, которые живут на этом сервере,
// и скромная форма пароля рядом. Управление — уже внутри.
//
// Наружу тут только имена доменов: они и так публичны (DNS, Certificate
// Transparency). Ни имён контейнеров, ни портов, ни ненастроенных доменов,
// ни имени сервера здесь нет — см. internal/server/public.go.
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

  return (
    <div className="mx-auto flex min-h-full w-full max-w-2xl flex-col justify-center gap-8 px-4 py-12">
      <header className="text-center">
        <div className="text-2xl font-semibold tracking-tight">EdgeDeck</div>
        <div className="mt-1 text-sm text-muted">сайты этого сервера</div>
      </header>

      <SiteList sites={sites} />

      <footer className="text-center">
        {showLogin ? (
          <PasswordForm onSuccess={onSuccess} onCancel={() => setShowLogin(false)} />
        ) : (
          <button
            onClick={() => setShowLogin(true)}
            className="rounded-lg border border-line px-4 py-2 text-sm text-muted hover:border-accent hover:text-text"
          >
            Управление
          </button>
        )}
      </footer>
    </div>
  );
}

function SiteList({ sites }: { sites: PublicSite[] | null }) {
  if (sites === null) {
    return <div className="text-center text-sm text-muted">загружаю…</div>;
  }
  if (sites.length === 0) {
    return (
      <div className="rounded-2xl border border-line bg-panel p-6 text-center text-sm text-muted">
        Пока ни одного сайта.
      </div>
    );
  }
  return (
    <div className="divide-y divide-line overflow-hidden rounded-2xl border border-line bg-panel">
      {sites.map((s) => (
        <a
          key={s.domain}
          href={`https://${s.domain}/`}
          className="flex items-center gap-3 px-5 py-4 hover:bg-panel-2"
        >
          <span
            className={"size-2 shrink-0 rounded-full " + (s.online ? "bg-ok" : "bg-muted")}
            title={s.online ? "отвечает" : "не отвечает"}
          />
          <span className="font-medium">{s.domain}</span>
          <span className="text-xs text-muted">{s.kind}</span>
          <span className="ml-auto text-xs text-muted">открыть →</span>
        </a>
      ))}
    </div>
  );
}

function PasswordForm({ onSuccess, onCancel }: { onSuccess: () => void; onCancel: () => void }) {
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
    <form onSubmit={submit} className="mx-auto w-full max-w-xs text-left">
      <label className="mb-2 block text-sm text-muted" htmlFor="pw">
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
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={busy || password.length === 0}
          className="flex-1 rounded-lg bg-accent px-3 py-2 font-medium text-ink disabled:opacity-40"
        >
          {busy ? "проверяю…" : "Войти"}
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="rounded-lg border border-line px-3 py-2 text-sm text-muted hover:text-text"
        >
          назад
        </button>
      </div>
    </form>
  );
}
