import createClient, { type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";
import { tokenStore } from "./token";

export type Canvas = components["schemas"]["Canvas"];
export type User = components["schemas"]["User"];
export type Membership = components["schemas"]["SpaceMembership"];
export type Me = components["schemas"]["Me"];
export type PlacedPixel = components["schemas"]["PlacedPixel"];
export type Template = components["schemas"]["Template"];
export type WSClientMessage = components["schemas"]["WSClientMessage"];
export type WSServerMessage = components["schemas"]["WSServerMessage"];

export const wsUrl = (ticket: string) =>
  `${baseUrl.replace(/^http/, "ws")}/ws?ticket=${encodeURIComponent(ticket)}`;

const baseUrl = import.meta.env.VITE_API_URL;
if (!baseUrl) throw new Error("VITE_API_URL is not set");

const auth: Middleware = {
  onRequest({ request }) {
    const token = tokenStore.get();
    if (token) request.headers.set("Authorization", `Bearer ${token}`);
    return request;
  },
  onResponse({ response }) {
    // Expired, revoked or suspended: drop the token so the app shows the login form.
    if (response.status === 401 && tokenStore.get()) tokenStore.set(null);
    return response;
  },
};

const client = createClient<paths>({ baseUrl });
client.use(auth);

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

// Too soon to place again; nextPlaceAt says when.
export class CooldownError extends ApiError {
  constructor(readonly nextPlaceAt: Date) {
    super(429, "cooldown");
  }
}

function fail(response: Response, error: { error: string }): never {
  throw new ApiError(response.status, error.error);
}

// The only code that talks to the API. Hooks call these, components call hooks.
export const authApi = {
  async login(email: string, password: string) {
    const { data, error, response } = await client.POST("/auth/login", { body: { email, password } });
    if (error) fail(response, error);
    return data;
  },
  async signup(body: { code: string; email: string; displayName: string; password: string }) {
    const { data, error, response } = await client.POST("/auth/signup", { body });
    if (error) fail(response, error);
    return data;
  },
  async logout() {
    await client.POST("/auth/logout");
  },
  async me(): Promise<Me> {
    const { data, error, response } = await client.GET("/me");
    if (error) fail(response, error);
    return data;
  },
};

export const realtimeApi = {
  async ticket(): Promise<string> {
    const { data, error, response } = await client.POST("/ws/ticket");
    if (error) fail(response, error);
    return data.ticket;
  },
};

export const spacesApi = {
  async join(code: string): Promise<Membership> {
    const { data, error, response } = await client.POST("/spaces/join", { body: { code } });
    if (error) fail(response, error);
    return data;
  },
  async createInvite(spaceId: string) {
    const { data, error, response } = await client.POST("/spaces/{spaceId}/invites", {
      params: { path: { spaceId } },
    });
    if (error) fail(response, error);
    return data;
  },
};

export const canvasesApi = {
  async list(spaceId: string): Promise<Canvas[]> {
    const { data, error, response } = await client.GET("/spaces/{spaceId}/canvases", {
      params: { path: { spaceId } },
    });
    if (error) fail(response, error);
    return data.canvases;
  },
  async create(spaceId: string, name: string): Promise<Canvas> {
    const { data, error, response } = await client.POST("/spaces/{spaceId}/canvases", {
      params: { path: { spaceId } },
      body: { name },
    });
    if (error) fail(response, error);
    return data;
  },
  async place(spaceId: string, canvasId: string, x: number, y: number, color: number): Promise<PlacedPixel> {
    const { data, error, response } = await client.POST("/spaces/{spaceId}/canvases/{canvasId}/pixels", {
      params: { path: { spaceId, canvasId } },
      body: { x, y, color },
    });
    if (response.status === 429 && error && "nextPlaceAt" in error) throw new CooldownError(new Date(error.nextPlaceAt));
    if (error) fail(response, error);
    return data;
  },

  // One byte per cell, row by row: an index into the canvas palette.
  async board(spaceId: string, canvasId: string): Promise<Uint8Array> {
    const { data, error, response } = await client.GET("/spaces/{spaceId}/canvases/{canvasId}/board", {
      params: { path: { spaceId, canvasId } },
      parseAs: "arrayBuffer",
    });
    if (error) fail(response, error);
    return new Uint8Array(data);
  },
};

export const templatesApi = {
  // Ready templates; each url is signed and expires at urlExpiresAt.
  async list(spaceId: string, canvasId: string): Promise<Template[]> {
    const { data, error, response } = await client.GET("/spaces/{spaceId}/canvases/{canvasId}/templates", {
      params: { path: { spaceId, canvasId } },
    });
    if (error) fail(response, error);
    return data.templates;
  },
  // Three steps: the API signs an upload URL, the file goes straight to the bucket, the API checks it.
  async upload(spaceId: string, canvasId: string, file: File, x: number, y: number): Promise<Template> {
    const path = { spaceId, canvasId };
    const start = await client.POST("/spaces/{spaceId}/canvases/{canvasId}/templates", {
      params: { path },
      body: { bytes: file.size, contentType: "image/png", x, y },
    });
    if (start.error) fail(start.response, start.error);
    const { templateId, uploadUrl, uploadHeaders } = start.data;

    // The one request that is not to our API: no bearer token, only the signed headers.
    const put = await fetch(uploadUrl, { method: "PUT", headers: uploadHeaders, body: file });
    if (!put.ok) throw new ApiError(put.status, "upload to storage failed");

    const done = await client.POST("/spaces/{spaceId}/canvases/{canvasId}/templates/{templateId}/complete", {
      params: { path: { ...path, templateId } },
    });
    if (done.error) fail(done.response, done.error);
    return done.data;
  },
  async remove(spaceId: string, canvasId: string, templateId: string): Promise<void> {
    const { error, response } = await client.DELETE("/spaces/{spaceId}/canvases/{canvasId}/templates/{templateId}", {
      params: { path: { spaceId, canvasId, templateId } },
    });
    if (error) fail(response, error);
  },
};
