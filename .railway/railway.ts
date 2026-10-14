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

  return project("rail-canvas", {
    variables: { managed: true },
    resources: [Postgres, api, postgresVolume, templates],
  });
});
