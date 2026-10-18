import { describe, expect, test } from "bun:test";
import { handle, type Env, type Fetch } from "./index.ts";

const env: Env = { telegramSecret: "tg-secret", apiURL: "http://api.test", webhookSecret: "wh-secret" };

type Call = { url: string; headers: Headers; body: any };

// A fake API: records each call and answers with the given status and body.
function fakeAPI(status: number, body: unknown = {}) {
  const calls: Call[] = [];
  const api: Fetch = async (url, init) => {
    calls.push({ url, headers: new Headers(init.headers), body: JSON.parse(String(init.body)) });
    return Response.json(body, { status });
  };
  return { api, calls };
}

function update(text: string, chatType = "private", secret: string | null = "tg-secret") {
  const headers: Record<string, string> = { "content-type": "application/json" };
  if (secret !== null) headers["x-telegram-bot-api-secret-token"] = secret;
  return new Request("http://bot.test/", {
    method: "POST",
    headers,
    body: JSON.stringify({ update_id: 1, message: { text, from: { id: 4242 }, chat: { id: 99, type: chatType } } }),
  });
}

async function replyText(res: Response): Promise<string> {
  expect(res.status).toBe(200);
  const b = await res.json();
  expect(b.method).toBe("sendMessage");
  expect(b.chat_id).toBe(99);
  return b.text;
}

const inSeconds = (s: number) => new Date(Date.now() + s * 1000).toISOString();

describe("the secret header", () => {
  for (const [name, secret] of [["missing", null], ["wrong", "nope"], ["empty", ""]] as const) {
    test(`${name}: 401 and the API is never called`, async () => {
      const { api, calls } = fakeAPI(201);
      const res = await handle(update("/place 1 2 red", "private", secret), env, api);
      expect(res.status).toBe(401);
      expect(calls).toHaveLength(0);
    });
  }

  test("not configured: everything refused", async () => {
    const { api, calls } = fakeAPI(201);
    const res = await handle(update("/help", "private", ""), { ...env, telegramSecret: "" }, api);
    expect(res.status).toBe(401);
    expect(calls).toHaveLength(0);
  });
});

describe("/place", () => {
  test("sends the right request and replies with the result", async () => {
    const { api, calls } = fakeAPI(201, { pixelId: 7, x: 10, y: 20, color: 5, colorName: "red", nextPlaceAt: inSeconds(10) });
    const text = await replyText(await handle(update("/place 10 20 red"), env, api));
    expect(text).toBe("Placed (10, 20) red. Next in 10 s");
    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe("http://api.test/webhooks/bot/pixels");
    expect(calls[0].headers.get("x-webhook-secret")).toBe("wh-secret");
    expect(calls[0].body).toEqual({ platform: "telegram", platformUserId: "4242", x: 10, y: 20, color: "red" });
  });

  test("a two-word colour and the bot's @name", async () => {
    const { api, calls } = fakeAPI(201, { colorName: "darkblue", nextPlaceAt: inSeconds(3) });
    await handle(update("/place@RailCanvasBot 1 2 dark blue"), env, api);
    expect(calls[0].body.color).toBe("dark blue");
  });

  for (const text of ["/place ten 20 red", "/place 1 -2 red", "/place 1 2", "/place 12345 1 red"]) {
    test(`"${text}": help, no call`, async () => {
      const { api, calls } = fakeAPI(201);
      expect(await replyText(await handle(update(text), env, api))).toContain("Commands:");
      expect(calls).toHaveLength(0);
    });
  }

  test("cooldown: Wait N s", async () => {
    const { api } = fakeAPI(429, { error: "cooldown", nextPlaceAt: inSeconds(7) });
    expect(await replyText(await handle(update("/place 1 2 red"), env, api))).toBe("Wait 7 s");
  });

  test("unknown colour lists the colours", async () => {
    const { api } = fakeAPI(400, { error: "unknown colour" });
    expect(await replyText(await handle(update("/place 1 2 mauve"), env, api))).toContain("Colours:");
  });

  test("not linked", async () => {
    const { api } = fakeAPI(404, { error: "this chat account is not linked" });
    expect(await replyText(await handle(update("/place 1 2 red"), env, api))).toContain("/link CODE");
  });

  test("the API down: 200 with try again", async () => {
    const api: Fetch = async () => {
      throw new TypeError("connection refused");
    };
    expect(await replyText(await handle(update("/place 1 2 red"), env, api))).toContain("try again");
  });

  test("the API's 500: 200 with try again", async () => {
    const { api } = fakeAPI(500, { error: "internal" });
    expect(await replyText(await handle(update("/place 1 2 red"), env, api))).toContain("try again");
  });
});

describe("/link", () => {
  test("redeems the code", async () => {
    const { api, calls } = fakeAPI(200, { displayName: "Ahmad", canvasName: "Main" });
    const text = await replyText(await handle(update("/link K7QM-2PXD"), env, api));
    expect(text).toStartWith('Linked as Ahmad, drawing on "Main".');
    expect(calls[0].url).toBe("http://api.test/webhooks/bot/link");
    expect(calls[0].body).toEqual({ platform: "telegram", platformUserId: "4242", code: "K7QM-2PXD" });
  });

  test("a bad code", async () => {
    const { api } = fakeAPI(404, { error: "invalid, used or expired link code" });
    expect(await replyText(await handle(update("/link AAAA-AAAA"), env, api))).toContain("wrong, used or expired");
  });

  test("too many tries", async () => {
    const { api } = fakeAPI(429, { error: "too many attempts" });
    expect(await replyText(await handle(update("/link AAAA-AAAA"), env, api))).toContain("Wait a minute");
  });
});

describe("everything else", () => {
  test("/help and unknown text get the help", async () => {
    const { api, calls } = fakeAPI(201);
    expect(await replyText(await handle(update("/help"), env, api))).toContain("Commands:");
    expect(await replyText(await handle(update("hello"), env, api))).toContain("Commands:");
    expect(calls).toHaveLength(0);
  });

  test("a group chat: 200, no reply, no call", async () => {
    const { api, calls } = fakeAPI(201);
    const res = await handle(update("/place 1 2 red", "group"), env, api);
    expect(res.status).toBe(200);
    expect(await res.text()).toBe("");
    expect(calls).toHaveLength(0);
  });

  test("not JSON: 200, no reply", async () => {
    const req = new Request("http://bot.test/", {
      method: "POST", headers: { "x-telegram-bot-api-secret-token": "tg-secret" }, body: "{not json",
    });
    const res = await handle(req, env, fakeAPI(201).api);
    expect(res.status).toBe(200);
    expect(await res.text()).toBe("");
  });

  test("a GET: 404", async () => {
    const res = await handle(new Request("http://bot.test/"), env, fakeAPI(201).api);
    expect(res.status).toBe(404);
  });
});
