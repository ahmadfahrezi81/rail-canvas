import { useState } from "react";
import type { Timelapse } from "../lib/api/client";

type Props = {
  timelapses: Timelapse[];
  onCreate: () => Promise<unknown>;
};

export function Timelapses({ timelapses, onCreate }: Props) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const pending = timelapses.some((t) => t.status === "pending");

  async function create() {
    setBusy(true);
    setError(undefined);
    try {
      await onCreate();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="templates timelapses">
      <h2>Timelapses</h2>
      {timelapses.length > 0 && (
        <ul>
          {timelapses.map((t) => (
            <li key={t.id}>
              <span className="grow">
                {new Date(t.createdAt).toLocaleString()}
                <span className="muted"> · </span>
                {t.status === "pending" && <span className="muted">Making…</span>}
                {t.status === "ready" && t.url && (
                  <a href={t.url} target="_blank" rel="noreferrer">Open GIF ({t.frames} hours)</a>
                )}
                {t.status === "failed" && <span className="muted">{t.error ?? "Failed"}</span>}
              </span>
            </li>
          ))}
        </ul>
      )}
      <div>
        <button disabled={busy || pending} onClick={create}>
          {pending ? "Making a timelapse…" : "Make timelapse"}
        </button>
        <span className="muted"> One frame per hour the canvas changed, up to ten days</span>
      </div>
      {error && <p className="error">{error}</p>}
    </section>
  );
}
