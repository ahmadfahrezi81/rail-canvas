import { useState } from "react";
import { Board } from "./components/Board";
import { CanvasList } from "./components/CanvasList";
import { Palette } from "./components/Palette";
import { useBoard } from "./lib/hooks/useBoard";
import { useCanvases } from "./lib/hooks/useCanvases";

export default function App() {
  const { canvases, error, isLoading, create } = useCanvases();
  const [selectedId, setSelectedId] = useState<string>();
  const [color, setColor] = useState(5);
  const [cell, setCell] = useState<{ x: number; y: number }>();

  const canvas = canvases?.find((c) => c.id === selectedId) ?? canvases?.[0];
  const { board, error: boardError } = useBoard(canvas?.id);

  return (
    <div className="app">
      <header>
        <h1>Rail Canvas</h1>
      </header>

      <aside>
        {isLoading && <p className="muted">Loading…</p>}
        {error && <p className="error">{error.message}</p>}
        {canvases && (
          <CanvasList
            canvases={canvases}
            selectedId={canvas?.id}
            onSelect={setSelectedId}
            onCreate={async (name) => setSelectedId((await create(name)).id)}
          />
        )}
      </aside>

      <main>
        {canvas && (
          <>
            {boardError && <p className="error">{boardError.message}</p>}
            {board ? (
              <Board canvas={canvas} board={board} onCellClick={(x, y) => setCell({ x, y })} />
            ) : (
              <div className="board-placeholder" />
            )}
            <Palette colors={canvas.palette.colors} selected={color} onSelect={setColor} />
            <p className="muted">
              {cell ? `x ${cell.x}, y ${cell.y}` : "Click a cell"} · placing arrives in Step 6
            </p>
          </>
        )}
        {canvases?.length === 0 && <p className="muted">No canvases yet. Create one.</p>}
      </main>
    </div>
  );
}
