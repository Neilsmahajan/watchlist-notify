"use client";

import { useId, useState } from "react";
import { Button, FIELD, Notice, cx } from "@/components/ui";
import { IconMail } from "@/components/icons";
import { redirectToLogin } from "@/lib/auth/client";
import { formatDate } from "@/lib/format";
import { readError } from "@/lib/watchlist/client";
import type { Me } from "@/lib/watchlist/types";

type Unit = "days" | "weeks" | "months";
const UNITS: Unit[] = ["days", "weeks", "months"];
const maxFor = (unit: Unit) => (unit === "days" ? 31 : 12);

type Form = {
  useAccountEmail: boolean;
  notifyEmail: string;
  digestEnabled: boolean;
  interval: number;
  unit: Unit;
  digestConsent: boolean;
  marketingConsent: boolean;
};

function formFrom(me: Me | null): Form {
  const prefs = me?.preferences;
  const unit = prefs?.digest?.interval_unit;
  return {
    useAccountEmail: prefs?.use_account_email ?? true,
    notifyEmail: prefs?.notify_email ?? "",
    digestEnabled: prefs?.digest?.enabled ?? false,
    interval: prefs?.digest?.interval || 1,
    unit: UNITS.includes(unit as Unit) ? (unit as Unit) : "weeks",
    digestConsent: prefs?.digest_consent ?? false,
    marketingConsent: prefs?.marketing_consent ?? false,
  };
}

function Check({
  checked,
  onChange,
  title,
  children,
}: {
  checked: boolean;
  onChange: (value: boolean) => void;
  title: string;
  children?: React.ReactNode;
}) {
  return (
    <label className="flex cursor-pointer items-start gap-3">
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
        className="mt-0.5 size-[18px] shrink-0 accent-[var(--accent)]"
      />
      <span className="min-w-0">
        <span className="block text-sm font-medium">{title}</span>
        {children && <span className="mt-0.5 block text-sm text-muted">{children}</span>}
      </span>
    </label>
  );
}

// Where digests go and how often. Saved with the button, not as you type.
export default function NotificationsSection({
  initial,
  initialError,
  accountEmail,
}: {
  initial: Me | null;
  initialError: string | null;
  accountEmail: string;
}) {
  const [me, setMe] = useState(initial);
  const [form, setForm] = useState(() => formFrom(initial));
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [notice, setNotice] = useState<{ tone: "ok" | "error" | "info"; text: string } | null>(
    initialError ? { tone: "error", text: initialError } : null,
  );
  const emailId = useId();
  const intervalId = useId();

  const set = <K extends keyof Form>(key: K, value: Form[K]) =>
    setForm((current) => ({ ...current, [key]: value }));

  const saved = formFrom(me);
  const dirty = (Object.keys(form) as (keyof Form)[]).some((key) => form[key] !== saved[key]);
  const digest = me?.preferences?.digest;
  const lastSent = formatDate(digest?.last_sent_at);
  const nextDue = formatDate(digest?.next_scheduled_at);

  const save = async () => {
    if (saving) return;
    const email = form.notifyEmail.trim();
    if (!form.useAccountEmail && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      setNotice({ tone: "error", text: "Enter the address digests should go to." });
      return;
    }
    if (form.digestEnabled && !form.digestConsent) {
      setNotice({ tone: "error", text: "Tick the box agreeing to receive digests to turn them on." });
      return;
    }
    const interval = Math.round(form.interval);
    if (!Number.isFinite(interval) || interval < 1 || interval > maxFor(form.unit)) {
      setNotice({
        tone: "error",
        text: `Choose between 1 and ${maxFor(form.unit)} ${form.unit}.`,
      });
      return;
    }

    const body: Record<string, unknown> = {};
    if (form.useAccountEmail !== saved.useAccountEmail) body.use_account_email = form.useAccountEmail;
    if (!form.useAccountEmail && email !== saved.notifyEmail) body.notify_email = email;
    if (form.digestEnabled !== saved.digestEnabled) body.digest_enabled = form.digestEnabled;
    // The API checks the number against the unit only when it gets both.
    if (interval !== saved.interval || form.unit !== saved.unit) {
      body.digest_interval = interval;
      body.digest_interval_unit = form.unit;
    }
    if (form.digestConsent !== saved.digestConsent) body.digest_consent = form.digestConsent;
    if (form.marketingConsent !== saved.marketingConsent) {
      body.marketing_consent = form.marketingConsent;
    }
    if (Object.keys(body).length === 0) {
      setNotice({ tone: "info", text: "Nothing to save." });
      return;
    }

    setSaving(true);
    setNotice(null);
    try {
      const response = await fetch("/api/me/preferences", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (response.status === 401) return redirectToLogin();
      if (!response.ok) throw new Error(await readError(response, "Your preferences could not be saved."));

      // Read the account back: the API works out the next digest date.
      const fresh = await fetch("/api/me", { headers: { Accept: "application/json" } });
      if (fresh.ok) {
        const data = (await fresh.json()) as Me;
        setMe(data);
        setForm(formFrom(data));
      }
      setNotice({ tone: "ok", text: "Saved." });
    } catch (cause) {
      setNotice({
        tone: "error",
        text: cause instanceof Error ? cause.message : "Your preferences could not be saved.",
      });
    } finally {
      setSaving(false);
    }
  };

  const sendTest = async () => {
    if (testing) return;
    setTesting(true);
    setNotice(null);
    try {
      const response = await fetch("/api/me/notifications/test", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ type: "digest" }),
      });
      if (response.status === 401) return redirectToLogin();
      if (!response.ok) throw new Error(await readError(response, "The test email could not be sent."));
      const data = (await response.json().catch(() => null)) as { sent_to?: string } | null;
      setNotice({
        tone: "ok",
        text: `Test digest sent to ${data?.sent_to || "your address"}. It can take a minute to arrive.`,
      });
    } catch (cause) {
      setNotice({
        tone: "error",
        text: cause instanceof Error ? cause.message : "The test email could not be sent.",
      });
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="space-y-8">
      <fieldset>
        <legend className="text-sm font-medium">Send digests to</legend>
        <div className="mt-3 space-y-3">
          <label className="flex cursor-pointer items-center gap-3">
            <input
              type="radio"
              name="digest-address"
              checked={form.useAccountEmail}
              onChange={() => set("useAccountEmail", true)}
              className="size-[18px] accent-[var(--accent)]"
            />
            <span className="min-w-0 text-sm">
              My account address
              {accountEmail && <span className="block truncate text-muted">{accountEmail}</span>}
            </span>
          </label>
          <label className="flex cursor-pointer items-center gap-3">
            <input
              type="radio"
              name="digest-address"
              checked={!form.useAccountEmail}
              onChange={() => set("useAccountEmail", false)}
              className="size-[18px] accent-[var(--accent)]"
            />
            <span className="text-sm">Another address</span>
          </label>
          {!form.useAccountEmail && (
            <div className="pl-[30px]">
              <label htmlFor={emailId} className="sr-only">
                Address for digests
              </label>
              <input
                id={emailId}
                type="email"
                inputMode="email"
                autoComplete="email"
                value={form.notifyEmail}
                onChange={(event) => set("notifyEmail", event.target.value)}
                placeholder="you@example.com"
                className={cx(FIELD, "max-w-sm")}
              />
            </div>
          )}
        </div>
      </fieldset>

      <div className="rounded-2xl border border-line bg-surface p-4 sm:p-5">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <p className="text-sm font-medium" id="digest-switch-label">
              Email digest
            </p>
            <p className="mt-0.5 text-sm text-muted">
              One email listing the titles on your list that are streaming on your services.
            </p>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={form.digestEnabled}
            aria-labelledby="digest-switch-label"
            onClick={() => set("digestEnabled", !form.digestEnabled)}
            className={cx(
              "relative h-7 w-12 shrink-0 rounded-full border transition-colors",
              form.digestEnabled ? "border-accent bg-accent" : "border-line-strong bg-surface-2",
            )}
          >
            <span
              className={cx(
                "absolute left-0.5 top-0.5 size-[22px] rounded-full shadow-sm transition-transform",
                form.digestEnabled ? "translate-x-5 bg-accent-ink" : "bg-fg/70",
              )}
            />
          </button>
        </div>

        {form.digestEnabled && (
          <div className="mt-5 space-y-5 border-t border-line pt-5">
            <div className="flex flex-wrap items-center gap-2.5">
              <label htmlFor={intervalId} className="text-sm">
                Send one every
              </label>
              <input
                id={intervalId}
                type="number"
                inputMode="numeric"
                min={1}
                max={maxFor(form.unit)}
                value={Number.isFinite(form.interval) ? form.interval : ""}
                onChange={(event) => set("interval", event.target.valueAsNumber)}
                className={cx(FIELD, "w-20! text-center")}
              />
              <select
                aria-label="Unit"
                value={form.unit}
                onChange={(event) => set("unit", event.target.value as Unit)}
                className={cx(FIELD, "w-auto! pr-8")}
              >
                {UNITS.map((unit) => (
                  <option key={unit} value={unit}>
                    {form.interval === 1 ? unit.slice(0, -1) : unit}
                  </option>
                ))}
              </select>
              <span className="text-xs text-muted">Up to {maxFor(form.unit)}</span>
            </div>
            <Check
              checked={form.digestConsent}
              onChange={(value) => set("digestConsent", value)}
              title="I agree to receive digest emails"
            >
              Needed before any digest is sent. Untick it to stop them.
            </Check>
          </div>
        )}

        {(lastSent || (saved.digestEnabled && nextDue)) && (
          <p className="slate mt-5 border-t border-line pt-4 text-muted">
            {[lastSent && `Last sent ${lastSent}`, saved.digestEnabled && nextDue && `Next due ${nextDue}`]
              .filter(Boolean)
              .join(" · ")}
          </p>
        )}
      </div>

      <Check
        checked={form.marketingConsent}
        onChange={(value) => set("marketingConsent", value)}
        title="Product news"
      >
        An occasional email when something new ships. Off unless you tick it.
      </Check>

      {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}

      <div className="flex flex-wrap gap-2.5">
        <Button onClick={() => void save()} loading={saving} disabled={!dirty && !saving}>
          Save changes
        </Button>
        <Button variant="secondary" onClick={() => void sendTest()} loading={testing} disabled={saving}>
          {!testing && <IconMail className="size-4" />}
          Send a test digest
        </Button>
      </div>
    </div>
  );
}
