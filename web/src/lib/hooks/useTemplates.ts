import useSWR from "swr";
import { ApiError, templatesApi, type Template } from "../api/client";

export const TEMPLATE_MAX_BYTES = 1 << 20;
export const TEMPLATE_MAX_SIDE = 256;

// Signed URLs last an hour; refetching every 45 minutes renews them before they expire.
const REFRESH_MS = 45 * 60 * 1000;

export function useTemplates(spaceId: string, canvasId: string) {
  const { data, error, mutate } = useSWR(
    ["templates", spaceId, canvasId],
    ([, s, c]) => templatesApi.list(s, c),
    { refreshInterval: REFRESH_MS, shouldRetryOnError: false },
  );

  async function upload(file: File, x: number, y: number): Promise<Template> {
    await checkLocally(file);
    const t = await templatesApi.upload(spaceId, canvasId, file, x, y);
    await mutate((list = []) => [t, ...list], { revalidate: false });
    return t;
  }

  async function remove(id: string) {
    await templatesApi.remove(spaceId, canvasId, id);
    await mutate((list = []) => list.filter((t) => t.id !== id), { revalidate: false });
  }

  // 503: the server has no bucket configured; the panel hides itself.
  const unavailable = error instanceof ApiError && error.status === 503;
  return { templates: data, error: unavailable ? undefined : error, unavailable, upload, remove };
}

// A quick message before uploading. The server's checks are the real ones.
async function checkLocally(file: File) {
  if (file.type !== "image/png") throw new Error("PNG only");
  if (file.size > TEMPLATE_MAX_BYTES) throw new Error("At most 1 MB");
  const bitmap = await createImageBitmap(file);
  const { width, height } = bitmap;
  bitmap.close();
  if (width > TEMPLATE_MAX_SIDE || height > TEMPLATE_MAX_SIDE) {
    throw new Error(`At most ${TEMPLATE_MAX_SIDE} × ${TEMPLATE_MAX_SIDE} pixels (this one is ${width} × ${height})`);
  }
}
