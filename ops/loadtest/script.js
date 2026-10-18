// One k6 virtual user is one player: loads the page, opens the live socket, then places a pixel at
// a random cell every 1–3 s, now and then reloading the board. Runs only against staging (Step 17).
// make loadtest players=N   (see README.md)
import http from "k6/http";
import { fail } from "k6";
import { SharedArray } from "k6/data";
import { Counter, Trend } from "k6/metrics";
import { clearTimeout, setTimeout } from "k6/timers";
import { WebSocket } from "k6/websockets";

const BASE = (__ENV.STAGING_URL || "").replace(/\/$/, "");
const PLAYERS = Number(__ENV.PLAYERS || 10);
if (!/staging|^http:\/\/(localhost|host\.docker\.internal)/.test(BASE)) {
  throw new Error(`refusing ${BASE}: staging or a local API only`);
}
// The owner's cap: a typo cannot start a big (billed) run.
if (!(PLAYERS >= 1 && PLAYERS <= 100)) throw new Error("PLAYERS must be 1 to 100");

const tokens = new SharedArray("tokens", () => JSON.parse(open("./.tokens.json")));
if (tokens.length < PLAYERS) throw new Error(`.tokens.json has ${tokens.length} tokens; run make loadtest-login`);

// POST …/pixels to the same pixel arriving on the player's own socket: the whole realtime path.
const placedToSeen = new Trend("placed_to_seen", true);
const pixelsPlaced = new Counter("pixels_placed");
const wsErrors = new Counter("ws_errors");

// The cooldown's 429 is the game's rule working, not an error.
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 429));

export const options = {
  scenarios: {
    players: {
      executor: "ramping-vus",
      stages: [
        { duration: __ENV.RAMP || "1m", target: PLAYERS },
        { duration: __ENV.HOLD || "2m", target: PLAYERS },
        { duration: "10s", target: 0 },
      ],
      gracefulRampDown: "10s",
      gracefulStop: "10s",
    },
  },
  // Decided before any run (task 017): zero errors.
  thresholds: {
    http_req_failed: ["rate==0"],
    http_req_duration: ["p(95)<300", "p(99)<800"],
    placed_to_seen: ["p(95)<500"],
    ws_errors: ["count==0"],
  },
  summaryTrendStats: ["avg", "p(50)", "p(95)", "p(99)", "max"],
};

const auth = (token) => ({ headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" } });
const rand = (n) => Math.floor(Math.random() * n);

// A fresh canvas per run, with a 2 s cooldown, so runs never share a board.
export function setup() {
  const a = auth(tokens[0]);
  const space = (http.get(`${BASE}/me`, a).json("spaces") || []).find((s) => s.slug === "staging-club");
  if (!space) fail("player-001 is not a member of staging-club");
  const name = `load test ${PLAYERS}p ${new Date().toISOString().slice(0, 16)}`;
  const res = http.post(`${BASE}/spaces/${space.id}/canvases`, JSON.stringify({ name, cooldownSeconds: 2 }), a);
  if (res.status !== 201) fail(`create canvas: ${res.status}`);
  return { space: space.id, canvas: res.json("id"), colors: res.json("palette.colors").length };
}

// One iteration is one visit of about a minute; then the player comes back (a new socket).
const VISIT_MS = Number(__ENV.VISIT_MS || 60000);

export default function (run) {
  const a = auth(tokens[__VU - 1]);
  const canvasPath = `${BASE}/spaces/${run.space}/canvases/${run.canvas}`;
  const board = () => http.get(`${canvasPath}/board`, { ...a, responseType: "binary", tags: { name: "board" } });

  http.get(`${BASE}/me`, { ...a, tags: { name: "me" } });
  http.get(`${BASE}/spaces/${run.space}/canvases`, { ...a, tags: { name: "canvases" } });
  board();
  const ticket = http.post(`${BASE}/ws/ticket`, null, { ...a, tags: { name: "ticket" } });
  if (ticket.status >= 300) return;

  const sent = new Map(); // pixelId → when its POST started
  const ws = new WebSocket(`${BASE.replace(/^http/, "ws")}/ws?ticket=${ticket.json("ticket")}`);
  let next;
  let closing = false;
  const act = () => {
    next = setTimeout(() => {
      if (Math.random() < 0.05) {
        board();
      } else {
        const started = Date.now();
        const body = JSON.stringify({ x: rand(256), y: rand(256), color: rand(run.colors) });
        const res = http.post(`${canvasPath}/pixels`, body, { ...a, tags: { name: "place" } });
        if (res.status === 201) {
          sent.set(res.json("pixelId"), started);
          pixelsPlaced.add(1);
        }
      }
      act();
    }, 1000 + Math.random() * 2000);
  };

  ws.onopen = () => ws.send(JSON.stringify({ type: "subscribe", spaceId: run.space, canvasId: run.canvas }));
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data);
    if (m.type === "subscribed") act();
    else if (m.type === "pixel:placed" && sent.has(m.pixelId)) {
      placedToSeen.add(Date.now() - sent.get(m.pixelId));
      sent.delete(m.pixelId);
    } else if (m.type === "error") wsErrors.add(1);
  };
  ws.onerror = () => {
    if (!closing) wsErrors.add(1);
  };
  setTimeout(() => {
    closing = true;
    clearTimeout(next);
    ws.close();
  }, VISIT_MS);
}

// Each run's numbers, kept for the results table (results/ is not committed).
export function handleSummary(data) {
  const stamp = new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19);
  return {
    stdout: textSummary(data),
    [`results/${PLAYERS}p-${stamp}.json`]: JSON.stringify(data, null, 2),
  };
}

function textSummary(data) {
  const m = data.metrics;
  const ms = (name, stat) => (m[name] ? `${Math.round(m[name].values[stat])} ms` : "-");
  const passed = Object.values(m).every((x) => !x.thresholds || Object.values(x.thresholds).every((t) => t.ok));
  return [
    "",
    `players            ${PLAYERS}`,
    `requests           ${m.http_reqs.values.count} (${m.http_reqs.values.rate.toFixed(1)}/s)`,
    `errors             ${(m.http_req_failed.values.rate * 100).toFixed(2)}%   ws errors ${m.ws_errors ? m.ws_errors.values.count : 0}`,
    `http p95 / p99     ${ms("http_req_duration", "p(95)")} / ${ms("http_req_duration", "p(99)")}`,
    `placed to seen p95 ${ms("placed_to_seen", "p(95)")}`,
    `pixels placed      ${m.pixels_placed ? m.pixels_placed.values.count : 0}`,
    `result             ${passed ? "PASS" : "FAIL"}`,
    "",
  ].join("\n");
}
