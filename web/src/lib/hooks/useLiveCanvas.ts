import { useEffect, useRef, useState } from "react";
import { canvasesApi, realtimeApi, wsUrl, type WSServerMessage } from "../api/client";

export type LiveStatus = "connecting" | "live" | "reconnecting";
type Pixel = { pixelId: number; x: number; y: number; color: number };

/**
 * Keeps one canvas live: ticket → socket → subscribe → load the board → replay
 * anything that arrived meanwhile. Reconnects with growing waits and reloads,
 * so a dropped message is never lost for long. Each cell keeps the newest
 * pixelId it has shown and ignores older ones, whatever order they arrive in.
 */
export function useLiveCanvas(
  spaceId: string,
  canvasId: string,
  width: number,
  onPixel: (x: number, y: number, color: number) => void,
) {
  const [board, setBoard] = useState<Uint8Array>();
  const [status, setStatus] = useState<LiveStatus>("connecting");
  const versions = useRef(new Map<number, number>());
  const boardRef = useRef<Uint8Array | undefined>(undefined);
  const onPixelRef = useRef(onPixel);
  onPixelRef.current = onPixel;

  // Applies one pixel if it is newer than what the cell shows. Returns whether it did.
  function apply(p: Pixel): boolean {
    const cell = p.y * width + p.x;
    if ((versions.current.get(cell) ?? 0) >= p.pixelId) return false;
    versions.current.set(cell, p.pixelId);
    if (boardRef.current) boardRef.current[cell] = p.color;
    onPixelRef.current(p.x, p.y, p.color);
    return true;
  }

  // Draws a pixel before the server confirms it. Returns an undo that restores
  // the cell, unless a newer pixel (someone else's) landed there meanwhile.
  function paintNow(x: number, y: number, color: number): () => void {
    const cell = y * width + x;
    const before = { color: boardRef.current?.[cell] ?? 0, version: versions.current.get(cell) ?? 0 };
    if (boardRef.current) boardRef.current[cell] = color;
    onPixelRef.current(x, y, color);
    return () => {
      if ((versions.current.get(cell) ?? 0) !== before.version) return;
      if (boardRef.current) boardRef.current[cell] = before.color;
      onPixelRef.current(x, y, before.color);
    };
  }

  useEffect(() => {
    let socket: WebSocket | undefined;
    let stopped = false;
    let attempt = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;

    async function connect() {
      if (stopped) return;
      let buffer: Pixel[] | null = [];
      try {
        const ticket = await realtimeApi.ticket();
        if (stopped) return;
        socket = new WebSocket(wsUrl(ticket));
      } catch {
        return retry();
      }
      const ws = socket;

      ws.onopen = () => ws.send(JSON.stringify({ type: "subscribe", spaceId, canvasId }));
      ws.onmessage = async (e) => {
        const m = JSON.parse(e.data) as WSServerMessage;
        if (m.type === "pixel:placed" && m.pixelId != null) {
          const p = { pixelId: m.pixelId, x: m.x!, y: m.y!, color: m.color! };
          if (buffer) buffer.push(p);
          else apply(p);
        } else if (m.type === "subscribed") {
          try {
            const fresh = await canvasesApi.board(spaceId, canvasId);
            if (stopped || ws !== socket) return;
            versions.current = new Map();
            boardRef.current = fresh;
            setBoard(fresh);
            const pending = buffer ?? [];
            buffer = null;
            pending.sort((a, b) => a.pixelId - b.pixelId).forEach(apply);
            attempt = 0;
            setStatus("live");
          } catch {
            ws.close();
          }
        }
      };
      ws.onclose = () => {
        if (ws === socket) retry();
      };
    }

    function retry() {
      if (stopped) return;
      setStatus("reconnecting");
      // 1, 2, 4 … 30 seconds, with jitter so many tabs do not reconnect in lockstep.
      const wait = Math.min(30_000, 1000 * 2 ** attempt++) * (0.5 + Math.random() / 2);
      timer = setTimeout(connect, wait);
    }

    setStatus("connecting");
    setBoard(undefined);
    connect();
    return () => {
      stopped = true;
      clearTimeout(timer);
      socket?.close();
    };
  }, [spaceId, canvasId]);

  return { board, status, apply, paintNow };
}
