"use client";

import { useState } from "react";
import { logoUrl } from "@/lib/tmdb";
import { serviceLogoPath } from "@/lib/services";
import { cx } from "@/components/ui";

type ServiceLogoProps = {
  code?: string;
  name: string;
  logoPath?: string;
  className?: string;
  // When the name is printed next to the logo, the image adds nothing for a
  // screen reader.
  labelled?: boolean;
};

// A service's logo, always with its name: as the image's label, or as text
// when there is no logo to show.
export default function ServiceLogo({
  code,
  name,
  logoPath,
  className = "size-7",
  labelled = false,
}: ServiceLogoProps) {
  const [failed, setFailed] = useState(false);
  const path = serviceLogoPath(code, logoPath);

  if (!path || failed) {
    return (
      <span
        title={name}
        aria-label={labelled ? undefined : name}
        aria-hidden={labelled || undefined}
        className={cx(
          "inline-flex shrink-0 items-center justify-center rounded-[22%] bg-fg text-[10px] font-semibold uppercase leading-none text-bg",
          className,
        )}
      >
        {name.replace(/[^A-Za-z0-9+]/g, "").slice(0, 2) || "?"}
      </span>
    );
  }

  return (
    <img
      src={logoUrl(path)}
      alt={labelled ? "" : name}
      title={name}
      width={92}
      height={92}
      loading="lazy"
      decoding="async"
      onError={() => setFailed(true)}
      className={cx("shrink-0 rounded-[22%] object-cover", className)}
    />
  );
}
