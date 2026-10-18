import { bucket, defineRailway, fn, github, postgres, preserve, project, redis, ref, service, volume } from "railway/iac";

// Production and staging, as code. Pulled from Railway 2026-10-14; change settings here, not in the dashboard.
// Staging (Step 16) runs the same stack from the staging branch, with its own data and no production secret.
export default defineRailway((ctx) => {
  const prod = ctx.isEnvironment("production");
  const branch = prod ? "main" : "staging";
  const repo = github("ahmadfahrezi81/rail-canvas", { branch, checkSuites: true });
  const Postgres = postgres("Postgres", { region: "asia-southeast1-eqsg3a" });
  Postgres.networking = { privateNetworkEndpoint: "postgres" };
  const postgresVolume = volume("postgres-volume", { alerts: { usage: { "100": {}, "80": {}, "95": {} } }, allowOnlineResize: true, region: "asia-southeast1-eqsg3a", sizeMB: 5000 });
  // Board cache, cooldown, limits, tickets (Step 11). Private network only; holds nothing that cannot be rebuilt.
  const Redis = redis("Redis", { region: "asia-southeast1-eqsg3a" });

  // Template images (Step 8). Private; the API signs URLs. Region is fixed for good: Singapore, beside the API.
  const templates = bucket("templates", { region: "sin" });
  const api = service("api", {
    // checkSuites is "Wait for CI": deploy only once the CI run on main is green.
    source: repo,
    start: "/app/api",
    // Runs after the build, before the new version starts; a failure stops the deploy.
    // Migrations, then the bucket's CORS from CORS_ORIGINS.
    preDeploy: "/app/cli predeploy",
    healthcheck: "/health",
    // Two copies: WebSockets reach each other through Redis pub/sub (realtime.Bus).
    replicas: { "asia-southeast1-eqsg3a": prod ? 2 : 1 },
    networking: { privateNetworkEndpoint: "rail-canvas" },
    env: {
      APP_DB_PASSWORD: preserve(),
      ...(prod
        ? {
            CORS_ORIGINS: preserve(),
            DATABASE_URL: preserve(),
            MIGRATE_DATABASE_URL: preserve(),
            // Shared with our own programs that call the webhook (Step 15's bot). Set by the owner.
            WEBHOOK_SECRET: preserve(),
          }
        : {
            // The exact preview address, not a wildcard: the bucket's CORS cannot match wildcards.
            CORS_ORIGINS: "https://staging.rail-canvas.pages.dev,http://localhost:5173",
            DATABASE_URL: "postgresql://app:${{APP_DB_PASSWORD}}@${{Postgres.PGHOST}}:${{Postgres.PGPORT}}/${{Postgres.PGDATABASE}}",
            MIGRATE_DATABASE_URL: Postgres.env.DATABASE_URL,
            // The fake data's owner, made by the pre-deploy seed. Set by the owner.
            SEED_OWNER_PASSWORD: preserve(),
          }),
      S3_BUCKET: ref(templates, "BUCKET"),
      S3_ACCESS_KEY_ID: ref(templates, "ACCESS_KEY_ID"),
      S3_SECRET_ACCESS_KEY: ref(templates, "SECRET_ACCESS_KEY"),
      S3_REGION: ref(templates, "REGION"),
      S3_ENDPOINT: ref(templates, "ENDPOINT"),
      REDIS_URL: Redis.env.REDIS_URL,
    },
  });

  // Background jobs from the queue in Redis (Step 12): same image as api, another start command.
  // Serves no HTTP, so no domain and no healthcheck. Every variable is a reference: nothing to paste.
  const worker = service("worker", {
    source: repo,
    start: "/app/worker",
    replicas: { "asia-southeast1-eqsg3a": 1 },
    env: {
      // The app role, like the API's: the worker follows RLS too.
      DATABASE_URL: api.env.DATABASE_URL,
      REDIS_URL: Redis.env.REDIS_URL,
      S3_BUCKET: ref(templates, "BUCKET"),
      S3_ACCESS_KEY_ID: ref(templates, "ACCESS_KEY_ID"),
      S3_SECRET_ACCESS_KEY: ref(templates, "SECRET_ACCESS_KEY"),
      S3_REGION: ref(templates, "REGION"),
      S3_ENDPOINT: ref(templates, "ENDPOINT"),
    },
  });

  // Cron services: Railway starts them on schedule (UTC); each run does its work and exits.
  // Railway drops checkSuites when it creates a service, so a new one needs a second apply,
  // planned after its variables are set: any dashboard change after a plan makes its apply fail.
  const snapshot = service("snapshot", {
    source: repo,
    start: "/app/cli snapshot",
    deploy: { cronSchedule: "0 * * * *", restartPolicyType: "NEVER" },
    replicas: { "asia-southeast1-eqsg3a": 1 },
    env: {
      // Queues the worker's job; without Redis it renders the snapshots itself.
      DATABASE_URL: api.env.DATABASE_URL,
      REDIS_URL: Redis.env.REDIS_URL,
      S3_BUCKET: ref(templates, "BUCKET"),
      S3_ACCESS_KEY_ID: ref(templates, "ACCESS_KEY_ID"),
      S3_SECRET_ACCESS_KEY: ref(templates, "SECRET_ACCESS_KEY"),
      S3_REGION: ref(templates, "REGION"),
      S3_ENDPOINT: ref(templates, "ENDPOINT"),
    },
  });
  const cleanup = service("cleanup", {
    source: repo,
    start: "/app/cli cleanup",
    // An hour after the backup, so last night's dump holds everything this deletes.
    deploy: { cronSchedule: "0 20 * * *", restartPolicyType: "NEVER" },
    replicas: { "asia-southeast1-eqsg3a": 1 },
    env: {
      DATABASE_URL: api.env.DATABASE_URL,
      S3_BUCKET: ref(templates, "BUCKET"),
      S3_ACCESS_KEY_ID: ref(templates, "ACCESS_KEY_ID"),
      S3_SECRET_ACCESS_KEY: ref(templates, "SECRET_ACCESS_KEY"),
      S3_REGION: ref(templates, "REGION"),
      S3_ENDPOINT: ref(templates, "ENDPOINT"),
      // healthchecks.io check "rail-canvas cleanup". Set by the owner. Staging's runs are not watched.
      ...(prod ? { HC_PING_URL: preserve() } : {}),
    },
  });

  // The Telegram bot (Step 15): a Railway Function, its code embedded from the file (no repository,
  // so no Wait for CI; CI tests it before the merge). Telegram reaches it on its public domain,
  // generated in the dashboard; it reaches the API only over the private network.
  const bot = fn("bot", {
    codeFile: "../functions/webhook/index.ts",
    runtime: "bun",
    replicas: { "asia-southeast1-eqsg3a": 1 },
    env: {
      API_URL: "http://rail-canvas.railway.internal:8080",
      WEBHOOK_SECRET: api.env.WEBHOOK_SECRET,
      // Telegram sends it back on every update. Set by the owner.
      TELEGRAM_SECRET_TOKEN: preserve(),
    },
  });

  // Nightly pg_dump to Cloudflare R2 (Step 9): a cron service that runs, uploads and exits.
  // 19:00 UTC is 03:00 in Singapore. A failed night is reported by healthchecks.io, not retried.
  const backup = service("backup", {
    source: repo,
    rootDirectory: "ops/backup",
    build: { watchPatterns: ["/ops/backup/**"] },
    deploy: { cronSchedule: "0 19 * * *", restartPolicyType: "NEVER" },
    replicas: { "asia-southeast1-eqsg3a": 1 },
    env: {
      // As postgres: a dump as app would be filtered by RLS.
      DATABASE_URL: Postgres.env.DATABASE_URL,
      R2_BUCKET: "rail-canvas-backups",
      R2_ACCOUNT_ID: preserve(),
      R2_ACCESS_KEY_ID: preserve(),
      R2_SECRET_ACCESS_KEY: preserve(),
      HC_PING_URL: preserve(),
    },
  });

  return project("rail-canvas", {
    variables: { managed: true },
    // No backup (staging's data is fake) and no bot (one Telegram bot calls one address) in staging.
    // Staging's databases and their volumes come from the dashboard (Sync): the file only adopts them.
    resources: [Postgres, api, templates, Redis, worker, snapshot, cleanup, ...(prod ? [postgresVolume, backup, bot] : [])],
  });
});
