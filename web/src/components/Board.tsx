import { forwardRef, useEffect, useImperativeHandle, useMemo, useRef, useState, type PointerEvent } from "react";
import type { Canvas } from "../lib/api/client";

export type BoardHandle = { paint: (x: number, y: number, color: number) => void };

type Props = {
  canvas: Canvas;
  board: Uint8Array;
  color: number; // the selected palette index, for the hover outline
  onCellClick: (x: number, y: number) => void;
  onHover: (cell: { x: number; y: number } | undefined) => void;
};

const MAX_SCALE = 40; // a cell at most 40 screen pixels
const GRID_FROM = 8; // grid lines once a cell is this wide
const DRAG_PX = 4; // movement past this turns a click into a pan

/**
 * The board is one width×height <canvas>, scaled and moved with a CSS transform,
 * so zoom and pan never redraw it. A second canvas on top draws the grid and the
 * hover outline at screen resolution.
 */
export const Board = forwardRef<BoardHandle, Props>(function Board({ canvas, board, color, onCellClick, onHover }, ref) {
  const viewportRef = useRef<HTMLDivElement>(null);
  const boardRef = useRef<HTMLCanvasElement>(null);
  const overlayRef = useRef<HTMLCanvasElement>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });
  const [view, setView] = useState({ scale: 0, x: 0, y: 0 });
  const [hover, setHover] = useState<{ x: number; y: number }>();
  const drag = useRef<{ startX: number; startY: number; viewX: number; viewY: number; moved: boolean }>(undefined);
  const { width, height } = canvas;

  const rgb = useMemo(() => canvas.palette.colors.map(hexToRgb), [canvas.palette.colors]);
  const fit = size.w && size.h ? Math.min(size.w / width, size.h / height) : 1;

  useImperativeHandle(ref, () => ({
    paint(x, y, c) {
      const ctx = boardRef.current?.getContext("2d");
      if (!ctx) return;
      ctx.fillStyle = canvas.palette.colors[c] ?? "#000";
      ctx.fillRect(x, y, 1, 1);
    },
  }));

  // Full draw, once per board load.
  useEffect(() => {
    const ctx = boardRef.current?.getContext("2d");
    if (!ctx) return;
    const img = ctx.createImageData(width, height);
    for (let i = 0; i < board.length; i++) {
      const [r, g, b] = rgb[board[i]] ?? [0, 0, 0];
      img.data.set([r, g, b, 255], i * 4);
    }
    ctx.putImageData(img, 0, 0);
  }, [board, rgb, width, height]);

  // Track the viewport size; start fitted and centred.
  useEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setSize({ w: e.contentRect.width, h: e.contentRect.height }));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  useEffect(() => {
    if (size.w && view.scale === 0) setView(clampView({ scale: fit, x: 0, y: 0 }));
  }, [size.w, fit]); // eslint-disable-line react-hooks/exhaustive-deps

  function clampView(v: { scale: number; x: number; y: number }) {
    const scale = Math.min(MAX_SCALE, Math.max(fit, v.scale));
    const bw = width * scale;
    const bh = height * scale;
    // Smaller than the viewport: centre it. Larger: never pan past an edge.
    const x = bw <= size.w ? (size.w - bw) / 2 : Math.min(0, Math.max(size.w - bw, v.x));
    const y = bh <= size.h ? (size.h - bh) / 2 : Math.min(0, Math.max(size.h - bh, v.y));
    return { scale, x, y };
  }

  // Zoom keeping the point under (px, py) still.
  function zoomAt(factor: number, px: number, py: number) {
    setView((v) => {
      const scale = Math.min(MAX_SCALE, Math.max(fit, v.scale * factor));
      const k = scale / v.scale;
      return clampView({ scale, x: px - (px - v.x) * k, y: py - (py - v.y) * k });
    });
  }

  // Wheel and trackpad pinch (a wheel event with ctrlKey). Not passive, so the page does not scroll.
  useEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const r = el.getBoundingClientRect();
      zoomAt(Math.exp(-e.deltaY * (e.ctrlKey ? 0.01 : 0.0015)), e.clientX - r.left, e.clientY - r.top);
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  });

  function cellAt(e: PointerEvent) {
    const r = viewportRef.current!.getBoundingClientRect();
    const x = Math.floor((e.clientX - r.left - view.x) / view.scale);
    const y = Math.floor((e.clientY - r.top - view.y) / view.scale);
    return x >= 0 && y >= 0 && x < width && y < height ? { x, y } : undefined;
  }

  function onPointerDown(e: PointerEvent<HTMLDivElement>) {
    e.currentTarget.setPointerCapture(e.pointerId);
    drag.current = { startX: e.clientX, startY: e.clientY, viewX: view.x, viewY: view.y, moved: false };
  }

  function onPointerMove(e: PointerEvent<HTMLDivElement>) {
    const d = drag.current;
    if (d) {
      const dx = e.clientX - d.startX;
      const dy = e.clientY - d.startY;
      if (!d.moved && Math.hypot(dx, dy) > DRAG_PX) d.moved = true;
      if (d.moved) setView(clampView({ scale: view.scale, x: d.viewX + dx, y: d.viewY + dy }));
    }
    const cell = cellAt(e);
    if (cell?.x !== hover?.x || cell?.y !== hover?.y) {
      setHover(cell);
      onHover(cell);
    }
  }

  function onPointerUp(e: PointerEvent<HTMLDivElement>) {
    const d = drag.current;
    drag.current = undefined;
    if (d && !d.moved) {
      const cell = cellAt(e);
      if (cell) onCellClick(cell.x, cell.y);
    }
  }

  // Grid and hover outline, redrawn on every view change. Cheap: only visible lines.
  useEffect(() => {
    const el = overlayRef.current;
    if (!el || !size.w) return;
    const dpr = window.devicePixelRatio || 1;
    el.width = size.w * dpr;
    el.height = size.h * dpr;
    const ctx = el.getContext("2d")!;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, size.w, size.h);
    const { scale, x: ox, y: oy } = view;

    if (scale >= GRID_FROM) {
      ctx.strokeStyle = "rgba(0, 0, 0, 0.12)";
      ctx.lineWidth = 1;
      ctx.beginPath();
      const x0 = Math.max(0, Math.floor(-ox / scale));
      const x1 = Math.min(width, Math.ceil((size.w - ox) / scale));
      const y0 = Math.max(0, Math.floor(-oy / scale));
      const y1 = Math.min(height, Math.ceil((size.h - oy) / scale));
      for (let i = x0; i <= x1; i++) {
        const px = Math.round(ox + i * scale) + 0.5;
        ctx.moveTo(px, oy + y0 * scale);
        ctx.lineTo(px, oy + y1 * scale);
      }
      for (let j = y0; j <= y1; j++) {
        const py = Math.round(oy + j * scale) + 0.5;
        ctx.moveTo(ox + x0 * scale, py);
        ctx.lineTo(ox + x1 * scale, py);
      }
      ctx.stroke();
    }

    if (hover) {
      const s = Math.max(scale, 2);
      ctx.lineWidth = 2;
      ctx.strokeStyle = canvas.palette.colors[color] ?? "#000";
      ctx.strokeRect(ox + hover.x * scale - 1, oy + hover.y * scale - 1, s + 2, s + 2);
      ctx.strokeStyle = "rgba(0, 0, 0, 0.6)";
      ctx.lineWidth = 1;
      ctx.strokeRect(ox + hover.x * scale - 2.5, oy + hover.y * scale - 2.5, s + 5, s + 5);
    }
  }, [view, size, hover, color, canvas.palette.colors, width, height]);

  const center = () => ({ x: size.w / 2, y: size.h / 2 });

  return (
    <div className="board-wrap">
      <div
        ref={viewportRef}
        className="board-viewport"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerLeave={() => {
          setHover(undefined);
          onHover(undefined);
        }}
      >
        <canvas
          ref={boardRef}
          className="board"
          width={width}
          height={height}
          style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale || fit})` }}
        />
        <canvas ref={overlayRef} className="board-overlay" />
      </div>
      <div className="zoom">
        <button aria-label="Zoom in" onClick={() => zoomAt(2, center().x, center().y)}>+</button>
        <button aria-label="Zoom out" onClick={() => zoomAt(0.5, center().x, center().y)}>−</button>
        <button onClick={() => setView(clampView({ scale: fit, x: 0, y: 0 }))}>Fit</button>
        <span className="muted">{Math.round((view.scale || fit) * 10) / 10}×</span>
      </div>
    </div>
  );
});

function hexToRgb(hex: string): [number, number, number] {
  const n = parseInt(hex.slice(1), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}
