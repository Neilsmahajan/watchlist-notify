import { ImageResponse } from "next/og";
import { BRAND, BrandTile } from "@/lib/brand-image";

export const alt = "Watchlist Notify: know what you can watch tonight";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

// Blank posters in the page's own colours: no artwork is fetched to draw the
// share image, so it cannot fail or go stale.
const WALL: { tone: string; on: boolean }[][] = [
  [
    { tone: "#2a2420", on: false },
    { tone: "#3a2f22", on: true },
    { tone: "#211d1a", on: false },
  ],
  [
    { tone: "#33291d", on: true },
    { tone: "#252120", on: false },
    { tone: "#3d3020", on: true },
  ],
  [
    { tone: "#221e1b", on: false },
    { tone: "#2e2620", on: true },
    { tone: "#37291a", on: false },
  ],
];

export default function OpengraphImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          background: BRAND.bg,
          color: BRAND.fg,
          position: "relative",
          overflow: "hidden",
        }}
      >
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            justifyContent: "space-between",
            padding: "64px 0 64px 72px",
            width: 720,
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 18 }}>
            <BrandTile size={56} radius={12} />
            <div style={{ display: "flex", fontSize: 30, fontWeight: 600, letterSpacing: -0.5 }}>
              Watchlist
              <span style={{ color: BRAND.accent, marginLeft: 9 }}>Notify</span>
            </div>
          </div>

          <div style={{ display: "flex", flexDirection: "column" }}>
            <div
              style={{
                display: "flex",
                flexDirection: "column",
                fontSize: 82,
                fontWeight: 700,
                lineHeight: 1.02,
                letterSpacing: -3,
              }}
            >
              <span>Know what you</span>
              <span>can watch</span>
              <span style={{ color: BRAND.accent }}>tonight.</span>
            </div>
            <div
              style={{
                display: "flex",
                marginTop: 28,
                fontSize: 29,
                lineHeight: 1.35,
                color: BRAND.muted,
                maxWidth: 600,
              }}
            >
              One watchlist, checked every day against the streaming services you already pay for.
            </div>
          </div>

          <div style={{ display: "flex", fontSize: 22, letterSpacing: 3, color: BRAND.muted }}>
            WATCHLISTNOTIFY.COM
          </div>
        </div>

        <div
          style={{
            position: "absolute",
            right: -70,
            top: -90,
            display: "flex",
            gap: 22,
            transform: "rotate(8deg)",
          }}
        >
          {WALL.map((column, c) => (
            <div
              key={c}
              style={{ display: "flex", flexDirection: "column", gap: 22, marginTop: [70, 0, 120][c] }}
            >
              {column.map((poster, r) => (
                <div
                  key={r}
                  style={{
                    width: 168,
                    height: 252,
                    borderRadius: 14,
                    background: poster.tone,
                    border: `1px solid ${BRAND.line}`,
                    display: "flex",
                    alignItems: "flex-end",
                    padding: 14,
                  }}
                >
                  {poster.on && (
                    <div
                      style={{
                        width: 30,
                        height: 30,
                        borderRadius: 8,
                        background: BRAND.accent,
                        display: "flex",
                      }}
                    />
                  )}
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    ),
    size,
  );
}
