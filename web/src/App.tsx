import { useCallback, useEffect, useState } from "react";
import { api, Unauthorized } from "./api";
import { Login } from "./components/Login";
import { Shell } from "./components/Shell";

export function App() {
  const [state, setState] = useState<"loading" | "guest" | "user">("loading");

  const check = useCallback(async () => {
    try {
      const s = await api.session();
      setState(s.authenticated ? "user" : "guest");
    } catch {
      setState("guest");
    }
  }, []);

  useEffect(() => {
    void check();
  }, [check]);

  // Сессия могла истечь, пока страница была открыта: любой 401 из глубины
  // приложения возвращает нас на форму входа, а не показывает пустой экран.
  useEffect(() => {
    const onRejection = (e: PromiseRejectionEvent) => {
      if (e.reason instanceof Unauthorized) setState("guest");
    };
    window.addEventListener("unhandledrejection", onRejection);
    return () => window.removeEventListener("unhandledrejection", onRejection);
  }, []);

  return (
    <>
      <div className="ambient" aria-hidden />
      {state === "loading" ? (
        <div className="grid min-h-dvh place-items-center">
          <span className="size-6 animate-spin rounded-full border-2 border-white/10 border-t-accent" />
        </div>
      ) : state === "guest" ? (
        <Login onSuccess={() => setState("user")} />
      ) : (
        <Shell onLogout={() => setState("guest")} />
      )}
    </>
  );
}
