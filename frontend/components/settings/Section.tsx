import type { ReactNode } from "react";

// One block of the settings page: what it is on the left, the controls on
// the right. Stacked on a phone.
export default function Section({
  id,
  title,
  description,
  children,
}: {
  id: string;
  title: string;
  description?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section
      id={id}
      aria-labelledby={`${id}-heading`}
      className="grid gap-x-12 gap-y-6 border-t border-line py-10 lg:grid-cols-[17rem_minmax(0,1fr)]"
    >
      <div>
        <h2 id={`${id}-heading`} className="display text-2xl">
          {title}
        </h2>
        {description && <p className="mt-2 text-sm leading-relaxed text-muted">{description}</p>}
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  );
}
