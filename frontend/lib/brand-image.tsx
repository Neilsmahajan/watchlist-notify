// The logo mark drawn for next/og, which renders a subset of CSS (flexbox,
// no SVG currentColor). Used by the favicon, the Apple icon and the share
// image, so the three cannot drift apart.

export const BRAND = {
  bg: "#0c0b0a",
  surface: "#1f1c19",
  line: "#453f38",
  fg: "#f3eee6",
  muted: "#a79f93",
  accent: "#f4b740",
} as const;

// A poster with a notification dot, on the dark tile every icon size shares.
export function BrandTile({ size, radius }: { size: number; radius: number }) {
  const poster = { width: size * 0.44, height: size * 0.64 };
  const stroke = Math.max(2, size * 0.07);
  const dot = size * 0.3;
  return (
    <div
      style={{
        width: size,
        height: size,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: BRAND.bg,
        borderRadius: radius,
        position: "relative",
      }}
    >
      <div
        style={{
          width: poster.width,
          height: poster.height,
          border: `${stroke}px solid ${BRAND.fg}`,
          borderRadius: size * 0.09,
          display: "flex",
        }}
      />
      <div
        style={{
          position: "absolute",
          width: dot,
          height: dot,
          borderRadius: dot,
          background: BRAND.accent,
          border: `${Math.max(1.5, size * 0.045)}px solid ${BRAND.bg}`,
          top: size * 0.08,
          left: size * 0.56,
          display: "flex",
        }}
      />
    </div>
  );
}
