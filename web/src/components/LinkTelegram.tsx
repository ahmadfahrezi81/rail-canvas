import { useState } from "react";
import { useBotLink } from "../lib/hooks/useBotLink";

export function LinkTelegram({ spaceId, canvasId }: { spaceId: string; canvasId: string }) {
  const { link, create } = useBotLink(spaceId, canvasId);
  const [error, setError] = useState<string>();
  return (
    <p className="link-telegram">
      <button onClick={() => create().then(() => setError(undefined), (e: Error) => setError(e.message))}>Link Telegram</button>
      {link && (
        <span>
          {" "}
          Send <code>/link {link.code}</code> to the bot{" "}
          <span className="muted">· single use, until {new Date(link.expiresAt).toLocaleTimeString()}</span>
        </span>
      )}
      {error && <span className="error"> {error}</span>}
    </p>
  );
}
