import { useState } from "react";
import { AuthForm } from "./components/AuthForm";
import { Board } from "./components/Board";
import { CanvasList } from "./components/CanvasList";
import { Palette } from "./components/Palette";
import { SpaceBar } from "./components/SpaceBar";
import { useBoard } from "./lib/hooks/useBoard";
import { useCanvases } from "./lib/hooks/useCanvases";
import { useSession } from "./lib/hooks/useSession";

export default function App() {
  const session = useSession();

  if (!session.loggedIn) return <AuthForm onLogin={session.login} onSignup={session.signup} />;
  if (session.error) return <p className="error page">{session.error.message}</p>;
  if (!session.me) return <p className="muted page">Loading…</p>;

  // Keyed on the user so switching accounts starts from a clean state.
  return <Workspace key={session.me.user.id} session={session} />;
}

function Workspace({ session }: { session: ReturnType<typeof useSession> }) {
  const me = session.me!;
  const [spaceId, setSpaceId] = useState(me.spaces[0]?.id);
  const space = me.spaces.find((s) => s.id === spaceId) ?? me.spaces[0];

  const { canvases, error, create } = useCanvases(space?.id);
  const [selectedId, setSelectedId] = useState<string>();
  const [color, setColor] = useState(5);
  const [cell, setCell] = useState<{ x: number; y: number }>();

  const canvas = canvases?.find((c) => c.id === selectedId) ?? canvases?.[0];
  const { board, error: boardError } = useBoard(space?.id, canvas?.id);

  function selectSpace(id: string) {
    setSpaceId(id);
    setSelectedId(undefined);
  }

  return (
    <div className="app">
      <SpaceBar me={me} spaceId={space?.id} onSelect={selectSpace} onJoin={session.join} onLogout={session.logout} />

      {!space ? (
        <p className="muted">You are not in a space yet. Join one with an invite code above.</p>
      ) : (
        <>
          <aside>
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
        </>
      )}
    </div>
  );
}
