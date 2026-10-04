// The session token, in localStorage (cookies cannot cross pages.dev and railway.app).
const KEY = "rail-canvas.token";
const listeners = new Set<() => void>();

function read(): string | null {
  try {
    return localStorage.getItem(KEY);
  } catch {
    return null;
  }
}

export const tokenStore = {
  get: read,
  set(token: string | null) {
    try {
      if (token) localStorage.setItem(KEY, token);
      else localStorage.removeItem(KEY);
    } catch {
      // storage blocked: the session lasts until reload
    }
    listeners.forEach((l) => l());
  },
  subscribe(listener: () => void) {
    listeners.add(listener);
    return () => listeners.delete(listener);
  },
};
