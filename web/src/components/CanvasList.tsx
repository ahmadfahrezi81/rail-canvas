import { useState, type FormEvent } from "react";
import type { Canvas, PaletteId } from "../lib/api/client";

const PALETTES: { id: PaletteId; label: string }[] = [
  { id: "classic16", label: "16 colours" },
  { id: "place32", label: "32 colours" },
];

type Props = {
  canvases: Canvas[];
  selectedId: string | undefined;
  onSelect: (id: string) => void;
  onCreate: (name: string, palette: PaletteId) => Promise<void>;
};

export function CanvasList({ canvases, selectedId, onSelect, onCreate }: Props) {
  const [name, setName] = useState("");
  const [palette, setPalette] = useState<PaletteId>("classic16");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await onCreate(name.trim(), palette);
      setName("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create canvas");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="canvas-list">
      <h2>Canvases</h2>
      <ul>
        {canvases.map((c) => (
          <li key={c.id}>
            <button aria-current={c.id === selectedId} onClick={() => onSelect(c.id)}>
              {c.name} <span className="muted">· {c.cooldownSeconds}s</span>
            </button>
          </li>
        ))}
      </ul>
      <form onSubmit={submit}>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="New canvas" maxLength={64} />
        <select value={palette} onChange={(e) => setPalette(e.target.value as PaletteId)} aria-label="Palette">
          {PALETTES.map((p) => (
            <option key={p.id} value={p.id}>
              {p.label}
            </option>
          ))}
        </select>
        <button type="submit" disabled={busy || !name.trim()}>
          {busy ? "…" : "Create"}
        </button>
      </form>
      {error && <p className="error">{error}</p>}
    </div>
  );
}
