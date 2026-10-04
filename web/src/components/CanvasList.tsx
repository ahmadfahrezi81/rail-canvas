import { useState, type FormEvent } from "react";
import type { Canvas } from "../lib/api/client";

type Props = {
  canvases: Canvas[];
  selectedId: string | undefined;
  onSelect: (id: string) => void;
  onCreate: (name: string) => Promise<void>;
};

export function CanvasList({ canvases, selectedId, onSelect, onCreate }: Props) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await onCreate(name.trim());
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
              {c.name}
            </button>
          </li>
        ))}
      </ul>
      <form onSubmit={submit}>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="New canvas" maxLength={64} />
        <button type="submit" disabled={busy || !name.trim()}>
          {busy ? "…" : "Create"}
        </button>
      </form>
      {error && <p className="error">{error}</p>}
    </div>
  );
}
