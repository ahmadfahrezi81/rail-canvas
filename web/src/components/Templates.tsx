import { useState, type FormEvent } from "react";
import type { Template } from "../lib/api/client";

type Props = {
  templates: Template[];
  shown: Set<string>;
  onToggle: (id: string) => void;
  opacity: number;
  onOpacity: (v: number) => void;
  onUpload: (file: File, x: number, y: number) => Promise<unknown>;
  onRemove: (id: string) => Promise<unknown>;
};

export function Templates({ templates, shown, onToggle, opacity, onOpacity, onUpload, onRemove }: Props) {
  const [file, setFile] = useState<File>();
  const [x, setX] = useState(0);
  const [y, setY] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function run(work: () => Promise<unknown>) {
    setBusy(true);
    setError(undefined);
    try {
      await work();
      return true;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong");
      return false;
    } finally {
      setBusy(false);
    }
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!file) return;
    if (await run(() => onUpload(file, x, y))) {
      setFile(undefined);
      (e.target as HTMLFormElement).reset();
    }
  }

  return (
    <section className="templates">
      <h2>Templates</h2>
      {templates.length > 0 && (
        <>
          <ul>
            {templates.map((t) => (
              <li key={t.id}>
                <input type="checkbox" checked={shown.has(t.id)} onChange={() => onToggle(t.id)} aria-label="Show" />
                <img src={t.thumbUrl ?? t.url} alt="" />
                <span className="grow">
                  {t.width} × {t.height} at {t.x}, {t.y} <span className="muted">· {t.uploadedBy.name}</span>
                </span>
                <button disabled={busy} onClick={() => run(() => onRemove(t.id))}>Delete</button>
              </li>
            ))}
          </ul>
          <label className="opacity">
            Opacity
            <input type="range" min={0.1} max={1} step={0.05} value={opacity} onChange={(e) => onOpacity(+e.target.value)} />
          </label>
        </>
      )}
      <form onSubmit={submit}>
        <input type="file" accept="image/png" onChange={(e) => setFile(e.target.files?.[0])} />
        <label>x <input type="number" min={0} max={255} value={x} onChange={(e) => setX(+e.target.value)} /></label>
        <label>y <input type="number" min={0} max={255} value={y} onChange={(e) => setY(+e.target.value)} /></label>
        <button type="submit" disabled={!file || busy}>{busy ? "Uploading…" : "Upload"}</button>
        <span className="muted">PNG, up to 256 × 256 and 1 MB</span>
      </form>
      {error && <p className="error">{error}</p>}
    </section>
  );
}
