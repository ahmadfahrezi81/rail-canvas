import useSWR from "swr";
import { ApiError, timelapsesApi, type Timelapse } from "../api/client";

// While one is being made, poll often; otherwise refetch before the signed URLs (an hour) expire.
const POLL_MS = 3000;
const REFRESH_MS = 45 * 60 * 1000;

export function useTimelapses(spaceId: string, canvasId: string) {
  const { data, error, mutate } = useSWR(
    ["timelapses", spaceId, canvasId],
    ([, s, c]) => timelapsesApi.list(s, c),
    {
      refreshInterval: (list?: Timelapse[]) => (list?.some((t) => t.status === "pending") ? POLL_MS : REFRESH_MS),
      shouldRetryOnError: false,
    },
  );

  async function create() {
    const t = await timelapsesApi.create(spaceId, canvasId);
    await mutate((list = []) => [t, ...list], { revalidate: false });
  }

  // 503 on the list: the server has no bucket configured; the panel hides itself.
  const unavailable = error instanceof ApiError && error.status === 503;
  return { timelapses: data, unavailable, create };
}
