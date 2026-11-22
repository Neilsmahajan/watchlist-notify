"use client";

import { useState } from "react";
import ServiceLogo from "@/components/ServiceLogo";
import { Button, Notice, cx } from "@/components/ui";
import { IconCheck } from "@/components/icons";
import { redirectToLogin } from "@/lib/auth/client";
import { accessLabel, serviceName } from "@/lib/services";
import { readError } from "@/lib/watchlist/client";
import type { Service } from "@/lib/watchlist/types";

// The catalogue of services, each a switch. A change is saved as it is made.
export default function ServicesSection({
  initial,
  initialError,
}: {
  initial: Service[];
  initialError: string | null;
}) {
  const [services, setServices] = useState(initial);
  const [pending, setPending] = useState<Record<string, boolean>>({});
  const [error, setError] = useState<string | null>(initialError);
  const [reloading, setReloading] = useState(false);

  const reload = async () => {
    setReloading(true);
    setError(null);
    try {
      const response = await fetch("/api/me/services", { headers: { Accept: "application/json" } });
      if (response.status === 401) return redirectToLogin();
      if (!response.ok) throw new Error(await readError(response, "Your services could not be loaded."));
      const data = (await response.json()) as { services?: Service[] };
      setServices(data.services ?? []);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Your services could not be loaded.");
    } finally {
      setReloading(false);
    }
  };

  const toggle = async (service: Service) => {
    if (pending[service.code]) return;
    const active = !service.active;
    const flip = (to: boolean) =>
      setServices((all) => all.map((s) => (s.code === service.code ? { ...s, active: to } : s)));

    setPending((p) => ({ ...p, [service.code]: true }));
    setError(null);
    flip(active); // shown at once, put back if the save fails
    try {
      const response = await fetch("/api/me/services", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ toggle: [{ code: service.code, active }] }),
      });
      if (response.status === 401) return redirectToLogin();
      if (!response.ok) {
        throw new Error(await readError(response, `${service.name} could not be updated.`));
      }
    } catch (cause) {
      flip(!active);
      setError(cause instanceof Error ? cause.message : `${service.name} could not be updated.`);
    } finally {
      setPending((p) => {
        const next = { ...p };
        delete next[service.code];
        return next;
      });
    }
  };

  const chosen = services.filter((service) => service.active).length;

  return (
    <div>
      {error && (
        <Notice
          tone="error"
          className="mb-4"
          action={
            services.length === 0 ? (
              <Button size="sm" variant="secondary" onClick={() => void reload()} loading={reloading}>
                Try again
              </Button>
            ) : undefined
          }
        >
          {error}
        </Notice>
      )}

      {services.length > 0 && (
        <>
          <p className="slate mb-3 text-muted" aria-live="polite">
            {chosen} of {services.length} chosen
          </p>
          <ul className="grid grid-cols-2 gap-2.5 sm:grid-cols-3">
            {services.map((service) => {
              const name = serviceName(service.code, undefined, service.name);
              return (
                <li key={service.code}>
                  <button
                    type="button"
                    role="switch"
                    aria-checked={service.active}
                    aria-busy={pending[service.code] || undefined}
                    onClick={() => void toggle(service)}
                    className={cx(
                      "flex h-full w-full items-center gap-2.5 rounded-2xl border p-2.5 text-left transition-colors sm:gap-3",
                      service.active
                        ? "border-accent bg-accent-soft"
                        : "border-line bg-surface hover:border-line-strong",
                    )}
                  >
                    <ServiceLogo
                      code={service.code}
                      name={name}
                      labelled
                      className={cx("size-9 sm:size-10", !service.active && "opacity-70 grayscale")}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block text-[13px] font-medium leading-tight [overflow-wrap:anywhere] line-clamp-2 sm:text-sm">
                        {name}
                      </span>
                      {service.access && (
                        <span className="mt-0.5 hidden truncate text-xs text-muted sm:block">
                          {accessLabel(service.access)}
                        </span>
                      )}
                    </span>
                    <span
                      aria-hidden="true"
                      className={cx(
                        "flex size-5 shrink-0 items-center justify-center rounded-full border",
                        service.active
                          ? "border-accent bg-accent text-accent-ink"
                          : "border-line-strong text-transparent",
                      )}
                    >
                      <IconCheck className="size-3.5" strokeWidth={2.5} />
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </>
      )}
    </div>
  );
}
