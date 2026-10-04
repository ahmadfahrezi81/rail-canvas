import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

export type Canvas = components["schemas"]["Canvas"];

const baseUrl = import.meta.env.VITE_API_URL;
if (!baseUrl) throw new Error("VITE_API_URL is not set");

const client = createClient<paths>({ baseUrl });

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

// The only place that talks to the API. Hooks call these, components call hooks.
export const canvasesApi = {
  async list(): Promise<Canvas[]> {
    const { data, error, response } = await client.GET("/canvases");
    if (error) throw new ApiError(response.status, error.error);
    return data.canvases;
  },

  async create(name: string): Promise<Canvas> {
    const { data, error, response } = await client.POST("/canvases", { body: { name } });
    if (error) throw new ApiError(response.status, error.error);
    return data;
  },

  // One byte per cell, row by row: an index into the canvas palette.
  async board(canvasId: string): Promise<Uint8Array> {
    const { data, error, response } = await client.GET("/canvases/{canvasId}/board", {
      params: { path: { canvasId } },
      parseAs: "arrayBuffer",
    });
    if (error) throw new ApiError(response.status, error.error);
    return new Uint8Array(data);
  },
};
