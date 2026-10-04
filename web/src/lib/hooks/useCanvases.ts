import useSWR from "swr";
import { canvasesApi, type Canvas } from "../api/client";

export function useCanvases(spaceId: string | undefined) {
  const { data, error, isLoading, mutate } = useSWR(spaceId ? ["canvases", spaceId] : null, ([, id]) =>
    canvasesApi.list(id),
  );

  async function create(name: string): Promise<Canvas> {
    const canvas = await canvasesApi.create(spaceId!, name);
    // Seed the list from the response instead of refetching.
    await mutate((list = []) => [canvas, ...list], { revalidate: false });
    return canvas;
  }

  return { canvases: data, error, isLoading, create };
}
