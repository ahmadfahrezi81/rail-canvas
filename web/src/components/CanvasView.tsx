import { useRef, useState } from "react";
import { CooldownError, type Canvas } from "../lib/api/client";
import { useLiveCanvas } from "../lib/hooks/useLiveCanvas";
import { usePlacePixel } from "../lib/hooks/usePlacePixel";
import { useTemplates } from "../lib/hooks/useTemplates";
import { useTimelapses } from "../lib/hooks/useTimelapses";
import { Board, type BoardHandle } from "./Board";
import { Palette } from "./Palette";
import { Templates } from "./Templates";
import { Timelapses } from "./Timelapses";

export function CanvasView({ spaceId, canvas }: { spaceId: string; canvas: Canvas }) {
  const boardRef = useRef<BoardHandle>(null);
  const { board, status, apply, paintNow } = useLiveCanvas(spaceId, canvas.id, canvas.width, (x, y, c) =>
    boardRef.current?.paint(x, y, c),
  );
  const { place, canPlace, secondsLeft } = usePlacePixel(spaceId, canvas.id, canvas.cooldownSeconds);
  const [color, setColor] = useState(5);
  const [hover, setHover] = useState<{ x: number; y: number }>();
  const [error, setError] = useState<string>();
  const { templates, unavailable, upload, remove } = useTemplates(spaceId, canvas.id);
  const lapses = useTimelapses(spaceId, canvas.id);
  const [shown, setShown] = useState<Set<string>>(new Set());
  const [opacity, setOpacity] = useState(0.5);
  const overlays = (templates ?? []).filter((t) => shown.has(t.id));

  function toggle(id: string) {
    setShown((s) => {
      const next = new Set(s);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }

  // Optimistic: draw first, then ask the server; undo if it says no.
  async function onCellClick(x: number, y: number) {
    if (!canPlace) return;
    setError(undefined);
    const undo = paintNow(x, y, color);
    try {
      const p = await place(x, y, color);
      apply({ pixelId: p.pixelId, x, y, color }); // records the version, so the socket's echo is ignored
    } catch (err) {
      undo();
      if (!(err instanceof CooldownError)) setError(err instanceof Error ? err.message : "Could not place");
    }
  }

  return (
    <>
      {board ? (
        <Board
          ref={boardRef}
          canvas={canvas}
          board={board}
          color={color}
          onCellClick={onCellClick}
          onHover={setHover}
          overlays={overlays}
          overlayOpacity={opacity}
        />
      ) : (
        <div className="board-placeholder" />
      )}
      <Palette colors={canvas.palette.colors} selected={color} onSelect={setColor} />
      <p className="status-line">
        <span className={`dot dot-${status}`} title={status} />
        <span>{status === "live" ? "Live" : status === "connecting" ? "Connecting…" : "Reconnecting…"}</span>
        <span className="muted">·</span>
        <span className={secondsLeft > 0 ? "muted" : "ready"}>
          {secondsLeft > 0 ? `Next pixel in ${secondsLeft}s` : "Ready to place"}
        </span>
        <span className="muted">· {hover ? `x ${hover.x}, y ${hover.y}` : "drag to pan, scroll to zoom"}</span>
      </p>
      {error && <p className="error">{error}</p>}
      {templates && !unavailable && (
        <Templates
          templates={templates}
          shown={shown}
          onToggle={toggle}
          opacity={opacity}
          onOpacity={setOpacity}
          onUpload={async (file, x, y) => {
            const t = await upload(file, x, y);
            setShown((s) => new Set(s).add(t.id)); // show what was just uploaded
          }}
          onRemove={remove}
        />
      )}
      {lapses.timelapses && !lapses.unavailable && (
        <Timelapses timelapses={lapses.timelapses} onCreate={lapses.create} />
      )}
    </>
  );
}
