import { useEffect, useState } from "react";
import { canvasesApi, CooldownError, type PlacedPixel } from "../api/client";

// Placing plus the cooldown clock. The clock starts on click (optimistic); the
// server's answer then corrects it. The server is the judge.
export function usePlacePixel(spaceId: string, canvasId: string, cooldownSeconds: number) {
  const [nextPlaceAt, setNextPlaceAt] = useState<Date>();
  const [busy, setBusy] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!nextPlaceAt || nextPlaceAt.getTime() <= now) return;
    const t = setTimeout(() => setNow(Date.now()), 250);
    return () => clearTimeout(t);
  }, [nextPlaceAt, now]);

  const secondsLeft = nextPlaceAt ? Math.max(0, Math.ceil((nextPlaceAt.getTime() - now) / 1000)) : 0;

  // Whether a click may place right now. Checked before drawing anything.
  const canPlace = !busy && secondsLeft === 0;

  async function place(x: number, y: number, color: number): Promise<PlacedPixel> {
    const before = nextPlaceAt;
    setNextPlaceAt(new Date(Date.now() + cooldownSeconds * 1000));
    setNow(Date.now());
    setBusy(true);
    try {
      const p = await canvasesApi.place(spaceId, canvasId, x, y, color);
      setNextPlaceAt(new Date(p.nextPlaceAt));
      setNow(Date.now());
      return p;
    } catch (err) {
      // Refused: the server's cooldown if it gave one, else the clock as it was.
      setNextPlaceAt(err instanceof CooldownError ? err.nextPlaceAt : before);
      setNow(Date.now());
      throw err;
    } finally {
      setBusy(false);
    }
  }

  return { place, canPlace, secondsLeft, busy };
}
