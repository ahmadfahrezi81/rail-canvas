import { useSyncExternalStore } from "react";
import useSWR, { useSWRConfig } from "swr";
import { authApi, spacesApi } from "../api/client";
import { tokenStore } from "../api/token";

export function useSession() {
  const token = useSyncExternalStore(tokenStore.subscribe, tokenStore.get);
  const { mutate: mutateAll } = useSWRConfig();
  // Keyed on the token, so a new login never shows the previous user's data.
  const { data: me, error, isLoading, mutate } = useSWR(token ? ["me", token] : null, () => authApi.me());

  async function login(email: string, password: string) {
    tokenStore.set((await authApi.login(email, password)).token);
  }

  async function signup(body: Parameters<typeof authApi.signup>[0]) {
    tokenStore.set((await authApi.signup(body)).token);
  }

  async function logout() {
    await authApi.logout().catch(() => {}); // the token is dropped either way
    tokenStore.set(null);
    await mutateAll(() => true, undefined, { revalidate: false }); // clear every cached response
  }

  async function join(code: string) {
    const space = await spacesApi.join(code);
    await mutate();
    return space;
  }

  return { loggedIn: !!token, me, error, isLoading, login, signup, logout, join };
}
