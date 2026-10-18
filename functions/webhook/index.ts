// The Telegram bot, a Railway Function (Bun, one file, no packages). Telegram posts each
// update here; this checks it came from Telegram, turns the command into a call to the API
// over Railway's private network, and replies inside the HTTP response, so it never needs
// the bot token. Task 015 has the reasons.
import { createHash, timingSafeEqual } from "node:crypto";

export type Env = {
  telegramSecret: string; // TELEGRAM_SECRET_TOKEN: Telegram sends it back on every update
  apiURL: string; // the API on the private network, http://rail-canvas.railway.internal:8080
  webhookSecret: string; // WEBHOOK_SECRET, a reference to the API's
};

export type Fetch = (url: string, init: RequestInit) => Promise<Response>;

export const MAX_BODY = 64 * 1024;
const API_TIMEOUT_MS = 5000;

const HELP = [
  "Rail Canvas bot. Commands:",
  "/link CODE: link this chat to your account (make the code with Link Telegram on a canvas page)",
  "/place X Y COLOUR: place a pixel, e.g. /place 10 20 red",
  "Colours: white, lightgrey, grey, black, pink, red, orange, brown, yellow, lime, green, cyan, blue, darkblue, magenta, purple, or 0 to 15",
].join("\n");

type Update = {
  message?: {
    text?: string;
    from?: { id: number };
    chat: { id: number; type: string };
  };
};

export async function handle(req: Request, env: Env, api: Fetch = fetch): Promise<Response> {
  if (req.method !== "POST") return new Response(null, { status: 404 });
  // Before reading the body: anyone on the internet can call this URL.
  if (!sameSecret(req.headers.get("x-telegram-bot-api-secret-token"), env.telegramSecret)) {
    log("refused", { reason: "secret" });
    return new Response(null, { status: 401 });
  }
  if (Number(req.headers.get("content-length") ?? 0) > MAX_BODY) return new Response(null, { status: 413 });

  let update: Update;
  try {
    update = (await req.json()) as Update;
  } catch {
    return ok(); // never a non-200 to Telegram: it would resend the same update for hours
  }
  const m = update.message;
  // Private chats only (group chats are out of scope).
  if (!m?.text || !m.from || m.chat?.type !== "private") return ok();

  const [command, ...args] = m.text.trim().split(/\s+/);
  const name = command.split("@")[0].toLowerCase(); // "/place@RailCanvasBot" in some clients
  const user = String(m.from.id);
  let text: string;
  try {
    if (name === "/link" && args.length === 1) text = await link(env, api, user, args[0]);
    else if (name === "/place" && args.length >= 3) text = await place(env, api, user, args);
    else text = HELP;
  } catch (err) {
    log("api failed", { command: name, err: String(err) });
    text = "Something went wrong, try again in a moment.";
  }
  return reply(m.chat.id, text);
}

async function link(env: Env, api: Fetch, user: string, code: string): Promise<string> {
  const res = await call(env, api, "/webhooks/bot/link", { platform: "telegram", platformUserId: user, code });
  log("link", { status: res.status });
  switch (res.status) {
    case 200: {
      const b = (await res.json()) as { displayName: string; canvasName: string };
      return `Linked as ${b.displayName}, drawing on "${b.canvasName}". Place with /place X Y COLOUR, e.g. /place 10 20 red`;
    }
    case 404:
      return "That code is wrong, used or expired. Make a new one with Link Telegram on the canvas page.";
    case 429:
      return "Too many tries. Wait a minute, then send the code again.";
  }
  throw new Error(`link: API answered ${res.status}`);
}

async function place(env: Env, api: Fetch, user: string, args: string[]): Promise<string> {
  const [xs, ys, ...colour] = args;
  if (!/^\d{1,4}$/.test(xs) || !/^\d{1,4}$/.test(ys)) return HELP;
  const x = Number(xs), y = Number(ys);
  const res = await call(env, api, "/webhooks/bot/pixels", {
    platform: "telegram", platformUserId: user, x, y, color: colour.join(" "),
  });
  log("place", { status: res.status });
  switch (res.status) {
    case 201: {
      const b = (await res.json()) as { colorName: string; nextPlaceAt: string };
      return `Placed (${x}, ${y}) ${b.colorName}. Next in ${secondsUntil(b.nextPlaceAt)} s`;
    }
    case 429: {
      const b = (await res.json()) as { nextPlaceAt: string };
      return `Wait ${secondsUntil(b.nextPlaceAt)} s`;
    }
    case 400: {
      const b = (await res.json()) as { error: string };
      return b.error === "unknown colour" ? `Unknown colour.\n${HELP}` : "That is outside the board.";
    }
    case 404: {
      const b = (await res.json()) as { error: string };
      return b.error.includes("not linked")
        ? "Link this chat first: /link CODE (make the code with Link Telegram on a canvas page)."
        : "Your canvas or membership is gone. Link again from a canvas page.";
    }
  }
  throw new Error(`place: API answered ${res.status}`);
}

function call(env: Env, api: Fetch, path: string, body: unknown): Promise<Response> {
  return api(env.apiURL + path, {
    method: "POST",
    headers: { "content-type": "application/json", "x-webhook-secret": env.webhookSecret },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(API_TIMEOUT_MS),
  });
}

// Hashes first, so neither the length nor the first wrong character shows in the timing.
function sameSecret(got: string | null, want: string): boolean {
  if (!got || !want) return false;
  const h = (s: string) => createHash("sha256").update(s).digest();
  return timingSafeEqual(h(got), h(want));
}

function secondsUntil(iso: string): number {
  return Math.max(1, Math.ceil((Date.parse(iso) - Date.now()) / 1000));
}

// Telegram reads a method call in the webhook's own response: no bot token needed.
function reply(chatId: number, text: string): Response {
  return Response.json({ method: "sendMessage", chat_id: chatId, text });
}

function ok(): Response {
  return new Response(null, { status: 200 });
}

// One JSON line per event, like the Go services. Never the secrets, never the message text.
function log(msg: string, fields: Record<string, unknown>) {
  console.log(JSON.stringify({ level: "INFO", msg, service: "bot", ...fields }));
}

if (import.meta.main) {
  const env: Env = {
    telegramSecret: process.env.TELEGRAM_SECRET_TOKEN ?? "",
    apiURL: process.env.API_URL ?? "",
    webhookSecret: process.env.WEBHOOK_SECRET ?? "",
  };
  if (!env.telegramSecret || !env.apiURL || !env.webhookSecret) {
    log("not configured: every update is refused", {
      telegram_secret: !!env.telegramSecret, api_url: !!env.apiURL, webhook_secret: !!env.webhookSecret,
    });
  }
  const server = Bun.serve({
    port: Number(process.env.PORT ?? 3000),
    maxRequestBodySize: MAX_BODY,
    fetch: (req) => handle(req, env),
  });
  log("listening", { port: server.port });
}
