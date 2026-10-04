import { useState } from "react";
import { spacesApi } from "../api/client";

// Not cached: every call makes a new single-use code.
export function useInvite(spaceId: string) {
  const [invite, setInvite] = useState<{ code: string; expiresAt: string }>();
  async function create() {
    setInvite(await spacesApi.createInvite(spaceId));
  }
  return { invite, create };
}
