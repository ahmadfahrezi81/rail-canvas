import { useState } from "react";
import { botApi } from "../api/client";

// Not cached: every call makes a new single-use code.
export function useBotLink(spaceId: string, canvasId: string) {
  const [link, setLink] = useState<{ code: string; expiresAt: string }>();
  async function create() {
    setLink(await botApi.linkCode(spaceId, canvasId));
  }
  return { link, create };
}
