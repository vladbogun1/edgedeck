import { useCallback, useEffect, useState } from "react";
import { api, Unauthorized } from "./api";
import { Login } from "./components/Login";
import { Shell } from "./components/Shell";

// Возврат после единого входа: сервис за входом прислал сюда с ?rd=<адрес>.
// Возвращаем только на https-адреса внутри домена единого входа — иначе это
// был бы открытый редирект на чужой сайт.
const returnTo = new URLSearchParams(window.location.search).get("rd") ?? "";

async function safeReturn(): Promise<string> {
  if (!returnTo) return "";
  try {
    const m = await api.loginMethods();
    const u = new URL(returnTo);
    const d = m.sso_domain;
    if (d && u.protocol === "https:" && (u.hostname === d || u.hostname.endsWith("." + d))) return u.toString();
  } catch {
    /* не вышло проверить — никуда не уводим */
  }
  return "";
}

export function App() {
  const [state, setState] = useState<"loading" | "guest" | "user">("loading");

  const afterLogin = useCallback(async () => {
    const to = await safeReturn();
    if (to) {
      window.location.replace(to);
      return;
    }
    setState("user");
  }, []);

  const check = useCallback(async () => {
    try {
      const s = await api.session();
      if (s.authenticated) {
        await afterLogin();
      } else {
        setState("guest");
      }
    } catch {
      setState("guest");
    }
  }, [afterLogin]);

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
        <Login onSuccess={() => void afterLogin()} autoOpen={Boolean(returnTo)} />
      ) : (
        <Shell onLogout={() => setState("guest")} />
      )}
    </>
  );
}
