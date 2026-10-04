import useSWR from "swr";
import { canvasesApi } from "../api/client";

// Fetched once per canvas. Live updates arrive over WebSocket in Step 6.
export function useBoard(canvasId: string | undefined) {
  const { data, error, isLoading } = useSWR(canvasId ? ["board", canvasId] : null, ([, id]) =>
    canvasesApi.board(id),
  );
  return { board: data, error, isLoading };
}
