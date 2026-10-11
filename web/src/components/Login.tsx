import { AnimatePresence, motion } from "framer-motion";
import { ArrowUpRight, CornerDownRight, KeyRound, Layers, Loader2, Lock, Send, ServerCog, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, type PublicSite } from "../api";
import { CountUp, SiteIcon, Skeleton, cn, hueOf, plural, rise, stackBy, stagger, subLabel, type Stack } from "../ui";

// Главная страница до входа: витрина сайтов этого сервера и вход в управление.
//
// Наружу тут только то, что публично и так: имена доменов и то, что сайты сами
// о себе публикуют (title, description, иконка, og:image). Ни контейнеров, ни
// портов, ни ненастроенных доменов, ни имени сервера — см. internal/server/public.go.
export function Login({ onSuccess, autoOpen = false }: { onSuccess: () => void; autoOpen?: boolean }) {
  const [sites, setSites] = useState<PublicSite[] | null>(null);
  // Пришли со своего сервиса за входом (?rd=) — окно входа сразу.
  const [showLogin, setShowLogin] = useState(autoOpen);

  useEffect(() => {
    void (async () => {
      try {
        setSites((await api.publicSites()).sites);
      } catch {
        setSites([]);
      }
    })();
  }, []);

  const liveSites = sites?.filter((s) => s.kind !== "редирект") ?? [];
  const services = liveSites.length;
  const projects = new Set(liveSites.map((s) => s.zone || s.domain)).size;
  const online = liveSites.filter((s) => s.online).length;
  const allUp = sites !== null && services > 0 && online === services;

  return (
    <div className="mx-auto flex min-h-dvh w-full max-w-6xl flex-col px-4 sm:px-6">
      <nav className="flex items-center justify-between py-5">
        <Brand />
        <button onClick={() => setShowLogin(true)} className="btn btn-ghost">
          <Lock className="size-3.5" />
          Управление
        </button>
      </nav>

      <header className="pt-10 pb-10 sm:pt-16 sm:pb-14">
        <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.4 }}>
          <div className="mb-5 inline-flex items-center gap-2 rounded-full border border-line bg-white/[0.03] px-3 py-1 text-xs text-muted backdrop-blur">
            <span className={cn("dot dot-live", allUp ? "text-ok" : sites === null ? "text-faint" : "text-warn")} />
            {sites === null
              ? "проверяю сайты…"
              : sites.length === 0
                ? "сайтов пока нет"
                : allUp
                  ? "все системы в норме"
                  : `работают ${online} из ${services}`}
          </div>
          <h1 className="page-title max-w-3xl text-4xl leading-[1.05] sm:text-6xl">
            Сайты <span className="text-gradient">этого сервера</span>
          </h1>
          <p className="mt-4 max-w-xl text-base text-muted">
            Всё, что здесь живёт, в одном месте: открывайте любой — статус проверяется прямо сейчас, тем же путём, что у
            посетителя.
          </p>
          {sites && sites.length > 0 && (
            <div className="mt-8 flex gap-8">
              <Stat label={plural(projects, "проект", "проекта", "проектов")} value={projects} />
              <Stat label={plural(services, "сервис", "сервиса", "сервисов")} value={services} />
              <Stat label="в сети" value={online} tone="ok" />
            </div>
          )}
        </motion.div>
      </header>

      <main className="flex-1 pb-16">
        <SiteGrid sites={sites} />
      </main>

      <footer className="border-t border-line py-6 text-xs text-faint">
        EdgeDeck · панель маршрутов этого сервера
      </footer>

      <AnimatePresence>
        {showLogin && <LoginDialog onSuccess={onSuccess} onClose={() => setShowLogin(false)} />}
      </AnimatePresence>
    </div>
  );
}

function Brand() {
  return (
    <div className="flex items-center gap-2.5">
      <span className="grid size-8 place-items-center rounded-[10px] bg-[image:var(--accent-grad)] text-[#04101f] shadow-[var(--glow-accent)]">
        <ServerCog className="size-4.5" strokeWidth={2.4} />
      </span>
      <span className="font-display text-[17px] font-bold tracking-tight">EdgeDeck</span>
    </div>
  );
}

function Stat({ label, value, tone }: { label: string; value: number; tone?: "ok" }) {
  return (
    <div>
      <div className={cn("kpi-num text-3xl", tone === "ok" && "text-ok")}>
        <CountUp value={value} />
      </div>
      <div className="mt-1 text-xs text-muted">{label}</div>
    </div>
  );
}

// Витрина группирует сервисы по зоне: chisetup.com.ua, app. и admin. — один
// проект, и показывать их тремя несвязанными карточками незачем. Редиректы
// отдельных карточек не получают: www уходит в свой стек подписью, старые
// адреса — в блок «Переадресации» внизу.
function splitSites(sites: PublicSite[]) {
  const live = sites.filter((s) => s.kind !== "редирект");
  const redirects = sites.filter((s) => s.kind === "редирект");
  const stacks = stackBy(
    live,
    (s) => s.zone,
    (s) => s.domain,
  );
  const liveDomains = new Set(live.map((s) => s.domain));
  // www.<сайт> → сам сайт — это просто второе имя, а не переадресация.
  const aliasOf = new Map<string, string[]>();
  const moved: PublicSite[] = [];
  for (const r of redirects) {
    const target = (r.redirect_to ?? "").replace(/^https?:\/\//, "");
    if (liveDomains.has(target) && r.domain === "www." + target) {
      aliasOf.set(target, [...(aliasOf.get(target) ?? []), "www"]);
    } else {
      moved.push(r);
    }
  }
  return { stacks, aliasOf, moved };
}

function SiteGrid({ sites }: { sites: PublicSite[] | null }) {
  if (sites === null) {
    return (
      <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
        <Skeleton className="h-[300px] rounded-[18px] sm:col-span-2 lg:col-span-3" />
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-[300px] rounded-[18px]" />
        ))}
      </div>
    );
  }
  if (sites.length === 0) {
    return (
      <div className="card hud px-6 py-16 text-center text-sm text-muted">
        Пока ни одного сайта. Направьте A-запись домена на этот сервер — и он появится здесь.
      </div>
    );
  }
  const { stacks, aliasOf, moved } = splitSites(sites);
  return (
    <div className="space-y-10">
      <motion.div
        variants={stagger}
        initial="hidden"
        animate="show"
        className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3"
      >
        {stacks.map((st) =>
          st.members.length > 1 ? (
            <motion.div key={st.zone} variants={rise} className="sm:col-span-2 lg:col-span-3">
              <StackCard stack={st} aliases={aliasOf} />
            </motion.div>
          ) : (
            <motion.div key={st.zone} variants={rise}>
              <SiteCard site={st.main} aliases={aliasOf.get(st.main.domain) ?? []} />
            </motion.div>
          ),
        )}
      </motion.div>
      {moved.length > 0 && <Redirects list={moved} />}
    </div>
  );
}

// Стек: превью главного сайта слева, сервисы проекта справа, связанные линией.
function StackCard({ stack, aliases }: { stack: Stack<PublicSite>; aliases: Map<string, string[]> }) {
  const main = stack.main;
  const hue = hueOf(stack.zone);
  const up = stack.members.filter((m) => m.online).length;
  const allUp = up === stack.members.length;
  const [imageOk, setImageOk] = useState(Boolean(main.image));
  const title = main.title && main.title !== main.domain ? main.title : prettyDomain(main.domain);
  const mainAliases = aliases.get(main.domain) ?? [];

  return (
    <div
      className="relative overflow-hidden rounded-[22px] border border-line bg-panel/80 backdrop-blur"
      style={{
        boxShadow: `0 30px 80px -40px hsl(${hue} 90% 50% / .45), var(--shadow-card)`,
      }}
    >
      <span
        className="absolute inset-x-0 top-0 z-10 h-px"
        style={{
          background: `linear-gradient(90deg, transparent, hsl(${hue} 90% 65% / .9), transparent)`,
        }}
      />
      <div className="grid lg:grid-cols-[1.15fr_1fr]">
        <a
          href={`https://${main.domain}/`}
          className="group relative block min-h-[240px] overflow-hidden border-b border-line lg:border-r lg:border-b-0"
        >
          {imageOk && main.image ? (
            <img
              src={main.image}
              alt=""
              loading="lazy"
              referrerPolicy="no-referrer"
              onError={() => setImageOk(false)}
              className="absolute inset-0 size-full object-cover transition-transform duration-700 group-hover:scale-[1.03]"
            />
          ) : (
            <GeneratedCover site={main} hue={hue} title={title} />
          )}
          {imageOk && main.image && (
            <>
              <div className="absolute inset-0 bg-gradient-to-t from-ink/95 via-ink/30 to-transparent" />
              <div className="absolute inset-x-0 bottom-0 flex items-end gap-3 p-5">
                <SiteIcon domain={main.domain} src={main.icon} size={46} />
                <div className="min-w-0">
                  <div className="truncate font-display text-xl font-bold text-white">{title}</div>
                  <div className="truncate font-mono text-xs text-white/70">
                    {main.domain}
                    {mainAliases.length > 0 && <span className="text-white/45"> · также {mainAliases.join(", ")}</span>}
                  </div>
                </div>
              </div>
            </>
          )}
        </a>

        <div className="flex flex-col p-5 sm:p-6">
          <div className="flex items-center gap-3">
            <span
              className="grid size-10 place-items-center rounded-xl border border-white/10"
              style={{
                background: `linear-gradient(135deg, hsl(${hue} 80% 55% / .4), hsl(${(hue + 50) % 360} 80% 45% / .2))`,
              }}
            >
              <Layers className="size-5 text-white" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="eyebrow">стек проекта</div>
              <div className="truncate font-display text-lg font-bold">{stack.zone}</div>
            </div>
            <span className={cn("chip", allUp ? "chip-ok" : "chip-warn")}>
              <span className={cn("dot !size-1.5", allUp && "dot-live")} />
              {allUp
                ? `${stack.members.length} ${plural(stack.members.length, "сервис", "сервиса", "сервисов")}`
                : `${up} из ${stack.members.length}`}
            </span>
          </div>
          {main.description && <p className="mt-3 line-clamp-2 text-sm text-muted">{main.description}</p>}

          <div className="relative mt-5 flex-1">
            <span
              className="absolute top-5 bottom-5 left-[24px] w-px"
              style={{
                background: `linear-gradient(to bottom, hsl(${hue} 80% 65% / .5), transparent)`,
              }}
              aria-hidden
            />
            <ul className="space-y-1">
              {stack.members.map((m) => (
                <li key={m.domain}>
                  <a
                    href={`https://${m.domain}/`}
                    className="group relative flex items-center gap-3 rounded-xl border border-transparent px-1.5 py-1.5 transition-colors hover:border-line hover:bg-white/[0.03]"
                  >
                    <SiteIcon
                      domain={m.domain}
                      src={m.icon}
                      size={36}
                      className="relative z-10 ring-4 ring-[var(--color-panel)]"
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span
                          className="shrink-0 rounded-md px-1.5 py-0.5 font-mono text-[11px] font-semibold"
                          style={{
                            background: `hsl(${hue} 80% 60% / .15)`,
                            color: `hsl(${hue} 90% 80%)`,
                          }}
                        >
                          {subLabel(m.domain, stack.zone)}
                        </span>
                        <span className="truncate text-sm font-medium">
                          {m.title && m.title !== m.domain ? m.title : m.domain}
                        </span>
                      </div>
                      <div className="truncate font-mono text-[11px] text-faint">
                        {m.domain}
                        {m !== main && (aliases.get(m.domain)?.length ?? 0) > 0 && " · также www"}
                      </div>
                    </div>
                    <span
                      className={cn("dot !size-2 shrink-0", m.online ? "text-ok" : "text-bad")}
                      title={m.online ? "работает" : "не отвечает"}
                    />
                    <ArrowUpRight className="size-4 shrink-0 text-faint transition-all group-hover:translate-x-0.5 group-hover:-translate-y-0.5 group-hover:text-accent-hi" />
                  </a>
                </li>
              ))}
            </ul>
          </div>
        </div>
      </div>
    </div>
  );
}

function Redirects({ list }: { list: PublicSite[] }) {
  const groups = stackBy(
    list,
    (s) => s.zone,
    (s) => s.domain,
  );
  return (
    <section>
      <div className="mb-3 flex items-center gap-3">
        <h2 className="eyebrow">Переадресации</h2>
        <span className="h-px flex-1 bg-line" />
      </div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {groups.map((g) => (
          <div key={g.zone} className="card px-4 py-3.5">
            <div className="mb-2 flex items-center gap-2">
              <SiteIcon domain={g.zone} size={20} />
              <span className="font-mono text-xs text-muted">{g.zone}</span>
            </div>
            <ul className="space-y-1.5">
              {g.members.map((r) => (
                <li key={r.domain}>
                  <a href={`https://${r.domain}/`} className="group flex items-center gap-2 text-[13px]">
                    <span className="truncate font-mono text-muted transition-colors group-hover:text-text">
                      {r.domain}
                    </span>
                    <CornerDownRight className="size-3.5 shrink-0 text-faint" />
                    <span className="truncate font-mono text-accent-hi">
                      {(r.redirect_to ?? "").replace(/^https?:\/\//, "") || "…"}
                    </span>
                  </a>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </section>
  );
}

function SiteCard({ site, aliases = [] }: { site: PublicSite; aliases?: string[] }) {
  const [imageOk, setImageOk] = useState(Boolean(site.image));
  const hue = hueOf(site.domain);
  const title = site.title && site.title !== site.domain ? site.title : prettyDomain(site.domain);

  return (
    <a
      href={`https://${site.domain}/`}
      className="group relative flex h-full flex-col overflow-hidden rounded-[18px] border border-line bg-panel/80 shadow-[var(--shadow-card)] backdrop-blur transition-[transform,border-color,box-shadow] duration-300 hover:-translate-y-1 hover:border-line-strong"
      style={{ ["--h" as string]: hue }}
    >
      {/* свечение карточки в цвет сайта при наведении */}
      <span
        className="pointer-events-none absolute -inset-px rounded-[18px] opacity-0 transition-opacity duration-300 group-hover:opacity-100"
        style={{
          boxShadow: `0 0 0 1px hsl(${hue} 90% 65% / .35), 0 20px 50px -20px hsl(${hue} 90% 55% / .5)`,
        }}
      />

      <div className="relative aspect-[16/9] overflow-hidden border-b border-line">
        {imageOk && site.image ? (
          <>
            <img
              src={site.image}
              alt=""
              loading="lazy"
              referrerPolicy="no-referrer"
              onError={() => setImageOk(false)}
              className="size-full object-cover transition-transform duration-500 group-hover:scale-[1.04]"
            />
            <div className="absolute inset-0 bg-gradient-to-t from-ink/60 via-transparent to-transparent" />
          </>
        ) : (
          <GeneratedCover site={site} hue={hue} title={title} />
        )}
        <span
          className={cn(
            "absolute top-3 right-3 flex items-center gap-1.5 rounded-full border border-white/10 bg-ink/70 px-2.5 py-1 text-[11.5px] font-medium backdrop-blur-md",
            site.online ? "text-ok" : "text-bad",
          )}
        >
          <span className={cn("dot", site.online && "dot-live")} />
          <span className="text-text">{site.online ? "работает" : "не отвечает"}</span>
        </span>
      </div>

      <div className="flex flex-1 flex-col p-5">
        <div className="flex items-start gap-3">
          <SiteIcon domain={site.domain} src={site.icon} size={38} />
          <div className="min-w-0 flex-1">
            <div className="truncate font-display text-[15.5px] font-semibold">{title}</div>
            <div className="truncate font-mono text-xs text-muted">
              {site.domain}
              {aliases.length > 0 && <span className="text-faint"> · также {aliases.join(", ")}</span>}
            </div>
          </div>
          <span className="grid size-8 shrink-0 place-items-center rounded-full border border-line text-muted transition-all duration-300 group-hover:border-accent/50 group-hover:bg-accent/10 group-hover:text-accent-hi">
            <ArrowUpRight className="size-4 transition-transform duration-300 group-hover:translate-x-0.5 group-hover:-translate-y-0.5" />
          </span>
        </div>
        {site.description ? (
          <p className="mt-3 line-clamp-2 text-sm leading-relaxed text-muted">{site.description}</p>
        ) : (
          <p className="mt-3 text-sm text-faint">{kindHint(site.kind)}</p>
        )}
        <div className="mt-auto pt-4">
          <span className="chip">{site.kind}</span>
        </div>
      </div>
    </a>
  );
}

// Обложка для сайта без og:image: мини-окно браузера в цвет сайта с его
// иконкой и названием. Выглядит как превью, а не как пустая плашка.
function GeneratedCover({ site, hue, title }: { site: PublicSite; hue: number; title: string }) {
  const h2 = (hue + 50) % 360;
  return (
    <div
      className="absolute inset-0"
      style={{
        background: `radial-gradient(90% 120% at 0% 0%, hsl(${hue} 85% 45% / .55), transparent 60%),
                     radial-gradient(80% 110% at 100% 100%, hsl(${h2} 85% 40% / .5), transparent 60%),
                     linear-gradient(160deg, hsl(${hue} 40% 12%), hsl(${h2} 40% 8%))`,
      }}
    >
      <div
        className="absolute inset-0 opacity-40"
        style={{
          backgroundImage:
            "linear-gradient(rgb(255 255 255 / .07) 1px, transparent 1px), linear-gradient(90deg, rgb(255 255 255 / .07) 1px, transparent 1px)",
          backgroundSize: "22px 22px",
          maskImage: "radial-gradient(ellipse at 50% 60%, #000 20%, transparent 75%)",
        }}
      />
      <div className="absolute inset-x-6 top-6 bottom-0 overflow-hidden rounded-t-xl border border-b-0 border-white/15 bg-ink/55 shadow-2xl backdrop-blur-md transition-transform duration-500 group-hover:translate-y-[-4px]">
        <div className="flex items-center gap-1.5 border-b border-white/10 px-3 py-2">
          <span className="size-2 rounded-full bg-white/25" />
          <span className="size-2 rounded-full bg-white/25" />
          <span className="size-2 rounded-full bg-white/25" />
          <span className="ml-2 flex-1 truncate rounded-md bg-white/8 px-2 py-0.5 font-mono text-[10px] text-white/60">
            {site.domain}
          </span>
        </div>
        <div className="flex items-center gap-3 px-4 pt-4">
          <SiteIcon domain={site.domain} src={site.icon} size={44} />
          <div className="min-w-0">
            <div className="truncate font-display text-sm font-semibold text-white">{title}</div>
            <div className="mt-1.5 h-1.5 w-24 rounded-full bg-white/15" />
          </div>
        </div>
        <div className="space-y-1.5 px-4 pt-4">
          <div className="h-1.5 w-11/12 rounded-full bg-white/10" />
          <div className="h-1.5 w-8/12 rounded-full bg-white/10" />
          <div className="mt-3 flex gap-2">
            <div className="h-5 w-16 rounded-md" style={{ background: `hsl(${hue} 85% 60% / .55)` }} />
            <div className="h-5 w-12 rounded-md bg-white/10" />
          </div>
        </div>
      </div>
    </div>
  );
}

function prettyDomain(d: string) {
  const name = d.replace(/^www\./, "").split(".")[0];
  return name.charAt(0).toUpperCase() + name.slice(1);
}

function kindHint(kind: string) {
  if (kind === "редирект") return "Перенаправляет на основной адрес.";
  if (kind === "статика") return "Статический сайт.";
  return "Веб-приложение на этом сервере.";
}

// ---- вход ----

function LoginDialog({ onSuccess, onClose }: { onSuccess: () => void; onClose: () => void }) {
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
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <motion.div
      className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-4 backdrop-blur-[6px]"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0, transition: { duration: 0.15 } }}
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <motion.div
        role="dialog"
        aria-modal="true"
        aria-label="Вход в управление"
        className="pop hud relative w-full max-w-sm p-6"
        initial={{ opacity: 0, y: 24, scale: 0.96 }}
        animate={{
          opacity: 1,
          y: 0,
          scale: 1,
          transition: { type: "spring", stiffness: 380, damping: 30 },
        }}
        exit={{
          opacity: 0,
          y: 12,
          scale: 0.98,
          transition: { duration: 0.15 },
        }}
      >
        <button
          onClick={onClose}
          aria-label="Закрыть"
          className="btn btn-ghost btn-icon btn-sm absolute top-4 right-4 !size-8"
        >
          <X className="size-4" />
        </button>
        <div className="mb-5 grid size-11 place-items-center rounded-xl border border-accent/30 bg-accent/10 text-accent-hi">
          {mode === "code" ? <Send className="size-5" /> : <KeyRound className="size-5" />}
        </div>
        {telegram === null ? (
          <Skeleton className="h-32" />
        ) : mode === "code" ? (
          <CodeForm onSuccess={onSuccess} />
        ) : (
          <PasswordForm onSuccess={onSuccess} />
        )}
        {telegram && (
          <button
            onClick={() => setMode(mode === "code" ? "password" : "code")}
            className="mt-5 w-full text-center text-xs text-muted transition-colors hover:text-text"
          >
            {mode === "code" ? "Войти паролем" : "Получить код в Telegram"}
          </button>
        )}
      </motion.div>
    </motion.div>
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
      input.current?.focus();
    } finally {
      setBusy(false);
    }
  }

  if (!challenge) {
    return (
      <div>
        <div className="font-display text-xl font-bold">Вход в управление</div>
        <p className="mt-1 mb-5 text-sm text-muted">Пришлю одноразовый код в ваш Telegram.</p>
        {error && <ErrorNote text={error} />}
        <button onClick={send} disabled={busy} className="btn btn-primary h-11 w-full">
          {busy ? <Loader2 className="size-4 animate-spin" /> : <Send className="size-4" />}
          {busy ? "Отправляю…" : "Получить код"}
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
      <div className="font-display text-xl font-bold">Код из Telegram</div>
      <p className="mt-1 mb-5 text-sm text-muted">Отправил. Код действует 5 минут.</p>
      <label htmlFor="otp" className="sr-only">
        Код
      </label>
      {/* Шесть ячеек поверх одного настоящего поля: автозаполнение кода из
          SMS/уведомлений и вставка работают как у обычного input. */}
      <div className="relative mb-4" onClick={() => input.current?.focus()}>
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
          className="absolute inset-0 size-full opacity-0"
        />
        <div className="grid grid-cols-6 gap-2">
          {Array.from({ length: 6 }, (_, i) => {
            const active = i === Math.min(code.length, 5);
            return (
              <div
                key={i}
                className={cn(
                  "grid h-13 place-items-center rounded-xl border bg-black/30 font-mono text-2xl font-semibold transition-all",
                  code[i] ? "border-line-strong text-text" : "border-line text-faint",
                  active && !busy && "border-accent shadow-[0_0_0_3px_rgb(91_157_255/0.18)]",
                )}
              >
                {code[i] ?? ""}
              </div>
            );
          })}
        </div>
      </div>
      {error && <ErrorNote text={error} />}
      <div className="flex gap-2">
        <button type="submit" disabled={busy || code.length !== 6} className="btn btn-primary h-11 flex-1">
          {busy && <Loader2 className="size-4 animate-spin" />}
          Войти
        </button>
        <button type="button" onClick={send} disabled={busy} className="btn btn-ghost h-11">
          Ещё код
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
      <div className="font-display text-xl font-bold">Вход по паролю</div>
      <p className="mt-1 mb-5 text-sm text-muted">Запасной способ, если Telegram недоступен.</p>
      <label className="field-label" htmlFor="pw">
        Пароль
      </label>
      <input
        id="pw"
        type="password"
        autoFocus
        autoComplete="current-password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        className="field mb-4 h-11"
      />
      {error && <ErrorNote text={error} />}
      <button type="submit" disabled={busy || password.length === 0} className="btn btn-primary h-11 w-full">
        {busy && <Loader2 className="size-4 animate-spin" />}
        Войти
      </button>
    </form>
  );
}

function ErrorNote({ text }: { text: string }) {
  return (
    <div role="alert" className="callout mb-4 text-bad [--tone:var(--color-bad)]">
      {text}
    </div>
  );
}
