import { useState, type FormEvent } from "react";
import type { Me } from "../lib/api/client";
import { useInvite } from "../lib/hooks/useInvite";

type Props = {
  me: Me;
  spaceId: string | undefined;
  onSelect: (id: string) => void;
  onJoin: (code: string) => Promise<{ id: string }>;
  onLogout: () => void;
};

export function SpaceBar({ me, spaceId, onSelect, onJoin, onLogout }: Props) {
  const space = me.spaces.find((s) => s.id === spaceId);
  return (
    <header className="space-bar">
      <h1>Rail Canvas</h1>
      {me.spaces.length > 1 ? (
        <select value={spaceId} onChange={(e) => onSelect(e.target.value)} aria-label="Space">
          {me.spaces.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      ) : (
        space && <span className="space-name">{space.name}</span>
      )}
      {space?.role === "owner" && <InviteButton key={space.id} spaceId={space.id} />}
      <JoinForm onJoin={async (code) => onSelect((await onJoin(code)).id)} />
      <span className="spacer" />
      <span className="muted">{me.user.displayName}</span>
      <button onClick={onLogout}>Log out</button>
    </header>
  );
}

function InviteButton({ spaceId }: { spaceId: string }) {
  const { invite, create } = useInvite(spaceId);
  const [error, setError] = useState<string>();
  return (
    <span className="invite">
      <button onClick={() => create().catch((e: Error) => setError(e.message))}>Invite</button>
      {invite && (
        <span>
          <code>{invite.code}</code> <span className="muted">single use, until {new Date(invite.expiresAt).toLocaleDateString()}</span>
        </span>
      )}
      {error && <span className="error">{error}</span>}
    </span>
  );
}

function JoinForm({ onJoin }: { onJoin: (code: string) => Promise<void> }) {
  const [code, setCode] = useState("");
  const [error, setError] = useState<string>();
  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(undefined);
    try {
      await onJoin(code);
      setCode("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not join");
    }
  }
  return (
    <form className="join" onSubmit={submit}>
      <input value={code} onChange={(e) => setCode(e.target.value)} placeholder="Invite code" aria-label="Invite code" />
      <button type="submit" disabled={!code.trim()}>
        Join
      </button>
      {error && <span className="error">{error}</span>}
    </form>
  );
}
