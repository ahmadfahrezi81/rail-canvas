import { useEffect, useMemo, useRef, type MouseEvent } from "react";
import type { Canvas } from "../lib/api/client";

type Props = {
  canvas: Canvas;
  board: Uint8Array;
  onCellClick: (x: number, y: number) => void;
};

export function Board({ canvas, board, onCellClick }: Props) {
  const ref = useRef<HTMLCanvasElement>(null);
  const rgb = useMemo(() => canvas.palette.colors.map(hexToRgb), [canvas.palette.colors]);

  // One ImageData write per board, not one element per cell.
  useEffect(() => {
    const ctx = ref.current?.getContext("2d");
    if (!ctx) return;
    const img = ctx.createImageData(canvas.width, canvas.height);
    for (let i = 0; i < board.length; i++) {
      const [r, g, b] = rgb[board[i]] ?? [0, 0, 0];
      img.data.set([r, g, b, 255], i * 4);
    }
    ctx.putImageData(img, 0, 0);
  }, [board, rgb, canvas.width, canvas.height]);

  function handleClick(e: MouseEvent<HTMLCanvasElement>) {
    const rect = e.currentTarget.getBoundingClientRect();
    const x = Math.floor(((e.clientX - rect.left) / rect.width) * canvas.width);
    const y = Math.floor(((e.clientY - rect.top) / rect.height) * canvas.height);
    onCellClick(x, y);
  }

  return (
    <canvas ref={ref} className="board" width={canvas.width} height={canvas.height} onClick={handleClick} />
  );
}

function hexToRgb(hex: string): [number, number, number] {
  const n = parseInt(hex.slice(1), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}
