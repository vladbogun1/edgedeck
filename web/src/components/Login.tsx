import { useState } from "react";
import { api } from "../api";

// Эту форму видно на любом домене, у которого ещё нет маршрута, и по IP тоже.
// Поэтому здесь нет ни имени сервера, ни числа доменов, ни версии — ничего,
// что рассказало бы постороннему, куда он попал (DESIGN.md §9.1).
export function Login({ onSuccess }: { onSuccess: () => void }) {
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
    <div className="grid h-full place-items-center px-4">
      <form
        onSubmit={submit}
        className="w-full max-w-sm rounded-2xl border border-line bg-panel p-8 shadow-2xl"
      >
        <div className="mb-6 text-center">
          <div className="text-2xl font-semibold tracking-tight">EdgeDeck</div>
        </div>
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
          className="mb-4 w-full rounded-lg border border-line bg-ink px-3 py-2 outline-none focus:border-accent"
        />
        {error && <div className="mb-4 rounded-lg bg-bad/10 px-3 py-2 text-sm text-bad">{error}</div>}
        <button
          type="submit"
          disabled={busy || password.length === 0}
          className="w-full rounded-lg bg-accent px-3 py-2 font-medium text-ink disabled:opacity-40"
        >
          {busy ? "проверяю…" : "Войти"}
        </button>
      </form>
    </div>
  );
}
