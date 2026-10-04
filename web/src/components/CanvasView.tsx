import { useRef, useState } from "react";
import { CooldownError, type Canvas } from "../lib/api/client";
import { useLiveCanvas } from "../lib/hooks/useLiveCanvas";
import { usePlacePixel } from "../lib/hooks/usePlacePixel";
import { Board, type BoardHandle } from "./Board";
import { Palette } from "./Palette";

export function CanvasView({ spaceId, canvas }: { spaceId: string; canvas: Canvas }) {
  const boardRef = useRef<BoardHandle>(null);
  const { board, status, apply, paintNow } = useLiveCanvas(spaceId, canvas.id, canvas.width, (x, y, c) =>
    boardRef.current?.paint(x, y, c),
  );
  const { place, canPlace, secondsLeft } = usePlacePixel(spaceId, canvas.id, canvas.cooldownSeconds);
  const [color, setColor] = useState(5);
  const [hover, setHover] = useState<{ x: number; y: number }>();
  const [error, setError] = useState<string>();

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
        <Board ref={boardRef} canvas={canvas} board={board} color={color} onCellClick={onCellClick} onHover={setHover} />
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
    </>
  );
}
