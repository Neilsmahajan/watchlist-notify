import Link from "next/link";
import type { ReactNode } from "react";

export function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(" ");
}

// Text inputs and selects. 16px on phones, so iOS does not zoom on focus.
// The shared look of a text field. A caller that needs another size overrides
// it with an important utility (w-20!), since class order does not decide.
export const FIELD =
  "h-11 w-full rounded-xl border border-line-strong bg-surface px-3.5 text-base text-fg placeholder:text-faint transition-colors focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30 disabled:opacity-60 sm:text-[15px]";

const BUTTON_BASE =
  "inline-flex items-center justify-center gap-2 rounded-full font-medium whitespace-nowrap transition-[background-color,border-color,color,transform] duration-150 active:scale-[0.98] disabled:opacity-50 disabled:active:scale-100";

const BUTTON_VARIANTS = {
  primary: "bg-accent text-accent-ink hover:bg-accent-hover",
  secondary:
    "border border-line-strong bg-surface text-fg hover:border-fg/40 hover:bg-surface-2",
  ghost: "text-muted hover:bg-surface-2 hover:text-fg",
  danger: "border border-danger/40 text-danger hover:bg-danger-soft",
  ink: "bg-fg text-bg hover:bg-fg/85",
} as const;

const BUTTON_SIZES = {
  sm: "h-8 px-3.5 text-[13px]",
  md: "h-10 px-5 text-sm",
  lg: "h-12 px-7 text-[15px]",
} as const;

type ButtonProps = {
  children: ReactNode;
  variant?: keyof typeof BUTTON_VARIANTS;
  size?: keyof typeof BUTTON_SIZES;
  href?: string;
  className?: string;
  loading?: boolean;
  disabled?: boolean;
  type?: "button" | "submit";
  onClick?: () => void;
  "aria-label"?: string;
  title?: string;
};

export function Button({
  children,
  variant = "primary",
  size = "md",
  href,
  className,
  loading = false,
  disabled = false,
  type = "button",
  ...rest
}: ButtonProps) {
  const classes = cx(
    BUTTON_BASE,
    BUTTON_VARIANTS[variant],
    BUTTON_SIZES[size],
    className,
  );

  if (href) {
    // Auth routes are served by middleware and external links leave the app:
    // both need a real navigation, not the client router.
    const plain = href.startsWith("/auth/") || /^[a-z]+:/i.test(href);
    if (plain) {
      const external = !href.startsWith("/");
      return (
        <a
          href={href}
          className={classes}
          {...(external ? { target: "_blank", rel: "noopener noreferrer" } : {})}
          aria-label={rest["aria-label"]}
          title={rest.title}
        >
          {children}
        </a>
      );
    }
    return (
      <Link
        href={href}
        className={classes}
        aria-label={rest["aria-label"]}
        title={rest.title}
      >
        {children}
      </Link>
    );
  }

  return (
    <button
      type={type}
      className={classes}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading && <Spinner className="size-4" />}
      {children}
    </button>
  );
}

export function Spinner({ className = "size-5" }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={cx("animate-spin", className)}
      fill="none"
      role="status"
      aria-label="Loading"
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="3" opacity="0.2" />
      <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  );
}

const NOTICE_TONES = {
  error: "border-danger/30 bg-danger-soft text-danger",
  ok: "border-ok/30 bg-ok-soft text-ok",
  info: "border-line bg-surface-2 text-muted",
  accent: "border-accent/40 bg-accent-soft text-fg",
} as const;

export function Notice({
  tone = "info",
  children,
  className,
  action,
}: {
  tone?: keyof typeof NOTICE_TONES;
  children: ReactNode;
  className?: string;
  action?: ReactNode;
}) {
  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      className={cx(
        "flex flex-wrap items-center justify-between gap-x-4 gap-y-2 rounded-xl border px-4 py-3 text-sm",
        NOTICE_TONES[tone],
        className,
      )}
    >
      <div className="min-w-0">{children}</div>
      {action}
    </div>
  );
}

export function EmptyState({
  title,
  children,
  action,
  className,
}: {
  title: string;
  children?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cx(
        "flex flex-col items-center rounded-2xl border border-dashed border-line-strong px-6 py-14 text-center",
        className,
      )}
    >
      {/* Three blank posters: the shelf waiting to be filled. */}
      <div className="mb-6 flex items-end gap-1.5" aria-hidden="true">
        <span className="h-12 w-8 -rotate-6 rounded-[5px] border border-line-strong bg-surface-2" />
        <span className="h-14 w-[38px] rounded-[5px] border border-line-strong bg-surface" />
        <span className="h-12 w-8 rotate-6 rounded-[5px] border border-line-strong bg-surface-2" />
      </div>
      <h2 className="display text-2xl">{title}</h2>
      {children && (
        <p className="mt-2 max-w-md text-sm leading-relaxed text-muted">
          {children}
        </p>
      )}
      {action && <div className="mt-6 flex flex-wrap justify-center gap-3">{action}</div>}
    </div>
  );
}

// The frame every signed-in page sits in.
export function PageShell({
  children,
  className,
  width = "wide",
}: {
  children: ReactNode;
  className?: string;
  width?: "wide" | "medium" | "narrow";
}) {
  return (
    <div
      className={cx(
        "mx-auto w-full px-4 pb-16 pt-8 sm:px-6 sm:pt-12 lg:px-8",
        width === "wide" ? "max-w-[1400px]" : width === "medium" ? "max-w-[1100px]" : "max-w-3xl",
        className,
      )}
    >
      {children}
    </div>
  );
}

export function PageHeading({
  eyebrow,
  title,
  children,
  action,
}: {
  eyebrow?: string;
  title: ReactNode;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <header className="mb-8 flex flex-wrap items-end justify-between gap-x-8 gap-y-4 sm:mb-10">
      <div className="min-w-0 max-w-3xl">
        {eyebrow && <p className="eyebrow mb-3">{eyebrow}</p>}
        <h1 className="display text-[2.1rem] sm:text-5xl">{title}</h1>
        {children && (
          <p className="mt-3 max-w-2xl text-[15px] leading-relaxed text-muted">
            {children}
          </p>
        )}
      </div>
      {action && <div className="flex flex-wrap gap-2">{action}</div>}
    </header>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      aria-hidden="true"
      className={cx("animate-shimmer rounded-lg bg-surface-2", className)}
    />
  );
}

// A grid of blank posters, shown while a list loads.
export function PosterGridSkeleton({ count = 12 }: { count?: number }) {
  return (
    <div className="poster-grid" aria-hidden="true">
      {Array.from({ length: count }, (_, i) => (
        <div key={i}>
          <Skeleton className="aspect-[2/3] w-full rounded-lg" />
          <Skeleton className="mt-2.5 h-3 w-4/5" />
          <Skeleton className="mt-1.5 h-2.5 w-2/5" />
        </div>
      ))}
    </div>
  );
}
