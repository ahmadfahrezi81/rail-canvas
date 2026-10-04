import { useState } from "react";
import { AuthForm } from "./components/AuthForm";
import { CanvasList } from "./components/CanvasList";
import { CanvasView } from "./components/CanvasView";
import { SpaceBar } from "./components/SpaceBar";
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
  const canvas = canvases?.find((c) => c.id === selectedId) ?? canvases?.[0];

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
            {canvas && <CanvasView key={canvas.id} spaceId={space.id} canvas={canvas} />}
            {canvases?.length === 0 && <p className="muted">No canvases yet. Create one.</p>}
          </main>
        </>
      )}
    </div>
  );
}
