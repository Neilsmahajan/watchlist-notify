// The streaming services the API knows, with the logo TMDB publishes for
// each. Names come from the API (GET /api/me/services); the ones here are the
// fallback for a code the page has no name for yet.

const SERVICES: Record<string, { name: string; logo: string }> = {
  netflix: { name: "Netflix", logo: "/rK1KljqmbvO9HQa1PBFLILWah72.png" },
  prime_video: { name: "Prime Video", logo: "/gMZdpavHmxFNnLpMHwVxfqeux2g.png" },
  hulu: { name: "Hulu", logo: "/44uAnmSqvA4yBOdbPWN8YgQHjWm.png" },
  disney_plus: { name: "Disney+", logo: "/5eZ872CghnHFLB1j8grszbrx0dx.png" },
  max: { name: "HBO Max", logo: "/skypuy7SXuugIQeYg0IglmzoKaS.png" },
  paramount_plus: { name: "Paramount+", logo: "/4N4BMd0Mm0kHAmF7RZgL5lW3cwc.png" },
  peacock: { name: "Peacock", logo: "/a1UIdq5BrkcAxnxcUhFsNbXnxeu.png" },
  apple_tv_plus: { name: "Apple TV", logo: "/v9SnyvYt0U3KGgBH3Sbh3z4yODe.png" },
  pluto_tv: { name: "Pluto TV", logo: "/fN4czqaMQNLeF6sSSIjGbAWzvwK.png" },
  tubi: { name: "Tubi", logo: "/9dEuvA8wg5TSeFBZlPxSVxFdimJ.png" },
  roku_channel: { name: "The Roku Channel", logo: "/b7uhrYJtAe7b1ieJGg705vTbwrX.png" },
  plex: { name: "Plex", logo: "/blAEhvx3XX8sV0fFVFk88FFjubs.png" },
  mgm_plus: { name: "MGM+", logo: "/q63Uzpu7JAs566vA2G23Lk7LcID.png" },
  crunchyroll: { name: "Crunchyroll", logo: "/uFL3c4Cq8M6WoLymlC5Y8bmGytV.png" },
  starz: { name: "Starz", logo: "/h25xjouKmiSmFiiqw0aDXbxGZo7.png" },
  amc_plus: { name: "AMC+", logo: "/wsCUflcmL4dCkzBUaGP8cj1cTkh.png" },
  espn_plus: { name: "ESPN+", logo: "/vred6xlUjlo9TL2NlmutsZnGHUT.png" },
  discovery_plus: { name: "Discovery+", logo: "/tHseJEgZaUdVlLtBpOGmKPfzbQ8.png" },
};

// Names that are shown differently from what an older API might send.
const NAME_OVERRIDES: Record<string, string> = {
  max: "HBO Max",
  apple_tv_plus: "Apple TV",
};

export const SERVICE_CODES = Object.keys(SERVICES);

export type ServiceNames = Record<string, string>;

export function serviceName(
  code: string | undefined,
  names?: ServiceNames,
  given?: string,
): string {
  if (!code) return given || "Unknown service";
  return (
    NAME_OVERRIDES[code] ||
    names?.[code] ||
    SERVICES[code]?.name ||
    given ||
    code
      .split("_")
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join(" ")
  );
}

// The TMDB logo path for a service, or for a provider the API sent a logo
// for. Undefined means there is no picture and the name must stand alone.
export function serviceLogoPath(
  code: string | undefined,
  given?: string,
): string | undefined {
  return (code && SERVICES[code]?.logo) || given || undefined;
}

export function accessLabel(access: string): string {
  switch (access) {
    case "subscription":
      return "Subscription";
    case "free":
      return "Free";
    case "ads":
      return "Free with ads";
    case "rent":
      return "Rent";
    case "buy":
      return "Buy";
    default:
      return access;
  }
}
