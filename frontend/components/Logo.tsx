// The mark is a poster with a notification dot: the two things this site is.

export function LogoMark({ className = "size-7" }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden="true">
      <rect
        x="7"
        y="5"
        width="16"
        height="24"
        rx="3"
        fill="none"
        stroke="currentColor"
        strokeWidth="2.5"
      />
      <path
        d="M11.5 22.5h7M11.5 18.5h4"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        opacity="0.45"
      />
      <circle cx="23" cy="7" r="5.5" fill="var(--accent)" stroke="var(--bg)" strokeWidth="2" />
    </svg>
  );
}

export function Wordmark({ className = "" }: { className?: string }) {
  return (
    <span className={`inline-flex items-baseline gap-[0.28em] ${className}`}>
      <span className="font-semibold tracking-tight">Watchlist</span>
      <span className="font-display italic font-medium tracking-tight text-accent-text">
        Notify
      </span>
    </span>
  );
}

export default function Logo({ className = "" }: { className?: string }) {
  return (
    <span className={`inline-flex items-center gap-2 ${className}`}>
      <LogoMark />
      <Wordmark className="text-[17px]" />
    </span>
  );
}
