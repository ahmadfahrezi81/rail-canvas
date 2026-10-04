import createClient, { type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";
import { tokenStore } from "./token";

export type Canvas = components["schemas"]["Canvas"];
export type User = components["schemas"]["User"];
export type Membership = components["schemas"]["SpaceMembership"];
export type Me = components["schemas"]["Me"];

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
