import { ImageResponse } from "next/og";
import { BrandTile } from "@/lib/brand-image";

export const size = { width: 64, height: 64 };
export const contentType = "image/png";

export default function Icon() {
  return new ImageResponse(<BrandTile size={64} radius={14} />, size);
}
