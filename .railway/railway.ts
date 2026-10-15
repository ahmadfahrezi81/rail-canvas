import { bucket, defineRailway, github, postgres, preserve, project, ref, service, volume } from "railway/iac";

// Production, as code. Pulled from Railway 2026-10-14; change settings here, not in the dashboard.
export default defineRailway(() => {
  const Postgres = postgres("Postgres", { region: "asia-southeast1-eqsg3a" });
  Postgres.networking = { privateNetworkEndpoint: "postgres" };
  const postgresVolume = volume("postgres-volume", { alerts: { usage: { "100": {}, "80": {}, "95": {} } }, allowOnlineResize: true, region: "asia-southeast1-eqsg3a", sizeMB: 5000 });
  // Template images (Step 8). Private; the API signs URLs. Region is fixed for good: Singapore, beside the API.
  const templates = bucket("templates", { region: "sin" });
  const api = service("api", {
    // checkSuites is "Wait for CI": deploy only once the CI run on main is green.
    source: github("ahmadfahrezi81/rail-canvas", { branch: "main", checkSuites: true }),
    start: "/app/api",
    // Runs after the build, before the new version starts; a failure stops the deploy.
    // Migrations, then the bucket's CORS from CORS_ORIGINS.
    preDeploy: "/app/cli predeploy",
    healthcheck: "/health",
    replicas: { "asia-southeast1-eqsg3a": 1 },
    networking: { privateNetworkEndpoint: "rail-canvas" },
    env: {
      APP_DB_PASSWORD: preserve(),
      CORS_ORIGINS: preserve(),
      DATABASE_URL: preserve(),
      MIGRATE_DATABASE_URL: preserve(),
      S3_BUCKET: ref(templates, "BUCKET"),
      S3_ACCESS_KEY_ID: ref(templates, "ACCESS_KEY_ID"),
      S3_SECRET_ACCESS_KEY: ref(templates, "SECRET_ACCESS_KEY"),
      S3_REGION: ref(templates, "REGION"),
      S3_ENDPOINT: ref(templates, "ENDPOINT"),
    },
  });

  // Nightly pg_dump to Cloudflare R2 (Step 9): a cron service that runs, uploads and exits.
  // 19:00 UTC is 03:00 in Singapore. A failed night is reported by healthchecks.io, not retried.
  const backup = service("backup", {
    source: github("ahmadfahrezi81/rail-canvas", { branch: "main", checkSuites: true }),
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
    resources: [Postgres, api, postgresVolume, templates, backup],
  });
});
