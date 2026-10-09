import { animate, motion, useInView, useMotionValue, useTransform, type Variants } from "framer-motion";
import { useEffect, useRef, useState, type ReactNode } from "react";

// Общие кирпичи интерфейса. Анимации — пружины с одним ритмом на всё
// приложение: появление снизу с лёгким масштабом, списки — лесенкой по 40 мс.

export const spring = { type: "spring", stiffness: 380, damping: 32 } as const;

export const stagger: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.04 } },
};

export const rise: Variants = {
  hidden: { opacity: 0, y: 14, scale: 0.98 },
  show: { opacity: 1, y: 0, scale: 1, transition: spring },
};

export function cn(...parts: (string | false | null | undefined)[]) {
  return parts.filter(Boolean).join(" ");
}

// Число, которое «досчитывает» до значения при первом появлении на экране.
export function CountUp({ value, decimals = 0 }: { value: number; decimals?: number }) {
  const ref = useRef<HTMLSpanElement>(null);
  const inView = useInView(ref, { once: true });
  const mv = useMotionValue(0);
  const text = useTransform(mv, (v) => v.toFixed(decimals));
  useEffect(() => {
    if (!inView) return;
    const c = animate(mv, value, { duration: 1.1, ease: [0.22, 1, 0.36, 1] });
    return () => c.stop();
  }, [inView, value, mv]);
  return <motion.span ref={ref}>{text}</motion.span>;
}

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 className="page-title text-2xl sm:text-[28px]">{title}</h1>
        {subtitle && <div className="mt-1 text-sm text-muted">{subtitle}</div>}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  );
}

export function Section({
  title,
  count,
  actions,
  children,
  hint,
}: {
  title: string;
  count?: number;
  actions?: ReactNode;
  hint?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section>
      <div className="mb-3 flex items-center gap-3">
        <h2 className="eyebrow">{title}</h2>
        {typeof count === "number" && (
          <span className="rounded-full bg-white/5 px-2 py-0.5 font-mono text-[11px] text-muted">{count}</span>
        )}
        <span className="h-px flex-1 bg-line" />
        {actions}
      </div>
      {children}
      {hint && <p className="mt-2 text-xs leading-relaxed text-faint">{hint}</p>}
    </section>
  );
}

export function Empty({ icon, title, children }: { icon: ReactNode; title: string; children?: ReactNode }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      className="card hud flex flex-col items-center px-6 py-12 text-center"
    >
      <div className="mb-4 grid size-12 place-items-center rounded-xl border border-accent/30 bg-accent/10 text-accent-hi">
        {icon}
      </div>
      <div className="font-display text-base font-semibold">{title}</div>
      {children && <div className="mt-1 max-w-md text-sm text-muted">{children}</div>}
    </motion.div>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("shimmer rounded-xl", className)} />;
}

export function SkeletonRows({ rows = 4 }: { rows?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-14" />
      ))}
    </div>
  );
}

export function StatusDot({ tone, live }: { tone: "ok" | "warn" | "bad" | "muted"; live?: boolean }) {
  const color = { ok: "text-ok", warn: "text-warn", bad: "text-bad", muted: "text-faint" }[tone];
  return <span className={cn("dot inline-block shrink-0", color, live && "dot-live")} />;
}

// Полоска заполнения: нагрузка, память, диск.
export function Meter({ value, max, tone }: { value: number; max: number; tone?: "ok" | "warn" | "bad" }) {
  const pct = max > 0 ? Math.min(100, (value / max) * 100) : 0;
  const t = tone ?? (pct > 90 ? "bad" : pct > 75 ? "warn" : "ok");
  const bg = { ok: "var(--accent-grad)", warn: "var(--color-warn)", bad: "var(--color-bad)" }[t];
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-white/6">
      <motion.div
        className="h-full rounded-full"
        style={{ background: bg }}
        initial={{ width: 0 }}
        animate={{ width: `${pct}%` }}
        transition={{ duration: 0.9, ease: [0.22, 1, 0.36, 1] }}
      />
    </div>
  );
}

// Цвет сайта из имени: у каждого свой и не меняется между загрузками.
// Палитра подобрана вручную — случайный hue давал грязные жёлто-зелёные.
const HUES = [215, 195, 255, 275, 170, 330, 230, 190, 300, 150];
export function hueOf(s: string) {
  let h = 0;
  for (const ch of s) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return HUES[h % HUES.length];
}

// Иконка сайта; если её нет или не загрузилась — буква на цветной плашке.
export function SiteIcon({ domain, src, size = 36, className }: { domain: string; src?: string; size?: number; className?: string }) {
  const [ok, setOk] = useState(Boolean(src));
  useEffect(() => setOk(Boolean(src)), [src]);
  const hue = hueOf(domain);
  const radius = Math.round(size * 0.28);
  if (ok && src) {
    return (
      <span
        className={cn("grid shrink-0 place-items-center overflow-hidden border border-white/10 bg-white/95", className)}
        style={{ width: size, height: size, borderRadius: radius }}
      >
        <img
          src={src}
          alt=""
          loading="lazy"
          referrerPolicy="no-referrer"
          onError={() => setOk(false)}
          className="size-[78%] object-contain"
        />
      </span>
    );
  }
  return (
    <span
      className={cn("grid shrink-0 place-items-center border border-white/15 font-display font-bold text-white uppercase", className)}
      style={{
        width: size,
        height: size,
        borderRadius: radius,
        fontSize: size * 0.42,
        background: `linear-gradient(135deg, hsl(${hue} 85% 62%), hsl(${(hue + 40) % 360} 80% 48%))`,
        boxShadow: `0 6px 20px -6px hsl(${hue} 90% 55% / .6)`,
      }}
    >
      {domain.replace(/^www\./, "")[0]}
    </span>
  );
}

export function plural(n: number, one: string, few: string, many: string) {
  const mod10 = n % 10;
  const mod100 = n % 100;
  if (mod10 === 1 && mod100 !== 11) return one;
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return few;
  return many;
}

// Переключатель-сегменты с плавающей подложкой (как в админке магазина).
export function Segmented<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { id: T; label: string; n?: number }[];
}) {
  return (
    <div className="inline-flex rounded-[11px] border border-line bg-black/25 p-[3px]">
      {options.map((o) => {
        const active = o.id === value;
        return (
          <button
            key={o.id}
            onClick={() => onChange(o.id)}
            className={cn(
              "relative flex h-8 items-center gap-1.5 rounded-lg px-3 text-[13px] font-medium transition-colors",
              active ? "text-accent-hi" : "text-muted hover:text-text",
            )}
          >
            {active && (
              <motion.span
                layoutId={`seg-${options.map((x) => x.id).join("")}`}
                transition={{ type: "spring", stiffness: 420, damping: 34 }}
                className="absolute inset-0 rounded-lg border border-accent/35 bg-accent/12"
              />
            )}
            <span className="relative">{o.label}</span>
            {typeof o.n === "number" && (
              <span className={cn("relative rounded-full px-1.5 font-mono text-[10.5px]", active ? "bg-accent/25" : "bg-white/8")}>
                {o.n}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
