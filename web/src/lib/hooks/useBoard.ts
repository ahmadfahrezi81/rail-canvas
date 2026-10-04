import useSWR from "swr";
import { canvasesApi } from "../api/client";

// Fetched once per canvas. Live updates arrive over WebSocket in Step 6.
export function useBoard(spaceId: string | undefined, canvasId: string | undefined) {
  const { data, error, isLoading } = useSWR(
    spaceId && canvasId ? ["board", spaceId, canvasId] : null,
    ([, s, c]) => canvasesApi.board(s, c),
  );
  return { board: data, error, isLoading };
}
