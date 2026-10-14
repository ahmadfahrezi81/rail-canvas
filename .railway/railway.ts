import { defineRailway, github, postgres, preserve, project, service, volume } from "railway/iac";

// Production, as code. Pulled from Railway 2026-10-14; change settings here, not in the dashboard.
export default defineRailway(() => {
  const Postgres = postgres("Postgres", { region: "asia-southeast1-eqsg3a" });
  Postgres.networking = { privateNetworkEndpoint: "postgres" };
  const postgresVolume = volume("postgres-volume", { alerts: { usage: { "100": {}, "80": {}, "95": {} } }, allowOnlineResize: true, region: "asia-southeast1-eqsg3a", sizeMB: 5000 });
  const api = service("api", {
    // checkSuites is "Wait for CI": deploy only once the CI run on main is green.
    source: github("ahmadfahrezi81/rail-canvas", { branch: "main", checkSuites: true }),
    start: "/app/api",
    // Runs after the build, before the new version starts; a failure stops the deploy.
    preDeploy: "/app/cli migrate",
    healthcheck: "/health",
    replicas: { "asia-southeast1-eqsg3a": 1 },
    networking: { privateNetworkEndpoint: "rail-canvas" },
    env: { APP_DB_PASSWORD: preserve(), CORS_ORIGINS: preserve(), DATABASE_URL: preserve(), MIGRATE_DATABASE_URL: preserve() },
  });

  return project("rail-canvas", {
    variables: { managed: true },
    resources: [Postgres, api, postgresVolume],
  });
});
