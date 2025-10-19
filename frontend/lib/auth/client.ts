export const redirectToLogin = (returnTo?: string) => {
  if (typeof window === "undefined") {
    return;
  }
  const target =
    returnTo && returnTo.trim().length
      ? returnTo
      : `${window.location.pathname}${window.location.search}`;
  const query = target ? `?returnTo=${encodeURIComponent(target)}` : "";
  window.location.href = `/auth/login${query}`;
};
