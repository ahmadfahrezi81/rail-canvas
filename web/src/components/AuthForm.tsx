import { useState, type FormEvent } from "react";

type Props = {
  onLogin: (email: string, password: string) => Promise<void>;
  onSignup: (body: { code: string; email: string; displayName: string; password: string }) => Promise<void>;
};

export function AuthForm({ onLogin, onSignup }: Props) {
  const [mode, setMode] = useState<"login" | "signup">("login");
  const [form, setForm] = useState({ code: "", email: "", displayName: "", password: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      if (mode === "login") await onLogin(form.email, form.password);
      else await onSignup(form);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong");
      setBusy(false);
    }
  }

  return (
    <form className="auth" onSubmit={submit}>
      <h1>Rail Canvas</h1>
      <div className="tabs" role="tablist">
        <button type="button" role="tab" aria-selected={mode === "login"} onClick={() => setMode("login")}>
          Log in
        </button>
        <button type="button" role="tab" aria-selected={mode === "signup"} onClick={() => setMode("signup")}>
          Sign up with invite
        </button>
      </div>
      {mode === "signup" && (
        <label>
          Invite code
          <input value={form.code} onChange={set("code")} required autoComplete="off" />
        </label>
      )}
      <label>
        Email
        <input type="email" value={form.email} onChange={set("email")} required autoComplete="email" />
      </label>
      {mode === "signup" && (
        <label>
          Display name
          <input value={form.displayName} onChange={set("displayName")} required maxLength={32} />
        </label>
      )}
      <label>
        Password
        <input
          type="password"
          value={form.password}
          onChange={set("password")}
          required
          minLength={mode === "signup" ? 10 : undefined}
          maxLength={72}
          autoComplete={mode === "login" ? "current-password" : "new-password"}
        />
      </label>
      <button type="submit" disabled={busy}>
        {busy ? "…" : mode === "login" ? "Log in" : "Create account"}
      </button>
      {error && <p className="error">{error}</p>}
    </form>
  );
}
