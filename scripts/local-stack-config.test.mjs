import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { parse } from "yaml";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function read(relativePath) {
  return readFileSync(resolve(root, relativePath), "utf8");
}

function envMap(service) {
  const environment = service.environment ?? {};
  if (!Array.isArray(environment)) return environment;

  return Object.fromEntries(
    environment.map((entry) => {
      const separator = entry.indexOf("=");
      return separator === -1
        ? [entry, null]
        : [entry.slice(0, separator), entry.slice(separator + 1)];
    }),
  );
}

function publishedPorts(service) {
  return (service.ports ?? []).map((port) =>
    typeof port === "string"
      ? port
      : `${port.host_ip ?? ""}:${port.published}:${port.target}`,
  );
}

const compose = parse(read("docker-compose.local.yml"));
const services = compose.services ?? {};

assert.equal(compose.name, "multica-local");
for (const name of [
  "postgres",
  "redis",
  "mailpit",
  "backend",
  "frontend",
]) {
  assert.ok(services[name], `missing local service: ${name}`);
}

assert.deepEqual(services.backend.build, {
  context: ".",
  dockerfile: "Dockerfile",
  args: {
    VERSION: "${VERSION:-local}",
    COMMIT: "${COMMIT:-local}",
    DATE: "${DATE:-local}",
  },
});
assert.deepEqual(services.frontend.build, {
  context: ".",
  dockerfile: "Dockerfile.web",
  args: { NEXT_PUBLIC_APP_VERSION: "${VERSION:-local}" },
});
assert.equal(services.backend.image, "multica-backend:local");
assert.equal(services.frontend.image, "multica-web:local");

const backendEnv = envMap(services.backend);
assert.match(backendEnv.DATABASE_URL, /@postgres:5432\//);
assert.equal(backendEnv.REDIS_URL, "redis://redis:6379/0");
assert.equal(backendEnv.SMTP_HOST, "mailpit");
assert.equal(backendEnv.SMTP_PORT, "1025");
assert.equal(backendEnv.LOCAL_UPLOAD_DIR, "/app/data/uploads");
assert.equal(backendEnv.ATTACHMENT_DOWNLOAD_MODE, "proxy");
assert.equal(backendEnv.ANALYTICS_DISABLED, "true");
assert.equal(backendEnv.POSTHOG_API_KEY, "");
assert.equal(backendEnv.APP_ENV, "production");
assert.equal(backendEnv.MULTICA_LLM_API_KEY, "");
assert.equal(backendEnv.MULTICA_LLM_BASE_URL, "");

const frontendEnv = envMap(services.frontend);
assert.equal(frontendEnv.REMOTE_API_URL, "http://backend:8080");

assert.deepEqual(services.backend.depends_on.postgres, {
  condition: "service_healthy",
});
assert.deepEqual(services.backend.depends_on.redis, {
  condition: "service_healthy",
});
assert.deepEqual(services.backend.depends_on.mailpit, {
  condition: "service_started",
});

for (const name of ["postgres", "redis", "mailpit", "backend", "frontend"]) {
  assert.ok(services[name].healthcheck, `${name} must have a healthcheck`);
  assert.equal(services[name].restart, "unless-stopped");
}

for (const name of ["backend", "frontend", "mailpit"]) {
  for (const port of publishedPorts(services[name])) {
    assert.match(port, /^127\.0\.0\.1:/, `${name} must bind only to loopback`);
  }
}

for (const volume of ["pgdata", "redisdata", "backend_uploads", "mailpitdata"]) {
  assert.ok(compose.volumes?.[volume] != null, `missing persistent volume: ${volume}`);
}

const aiCompose = parse(read("docker-compose.local-ai.yml"));
assert.ok(aiCompose.services?.ollama, "local AI overlay must provide Ollama");
assert.ok(aiCompose.services?.["ollama-model"], "local AI overlay must pull a model");
const aiBackendEnv = envMap(aiCompose.services?.backend ?? {});
assert.equal(aiBackendEnv.MULTICA_LLM_BASE_URL, "http://ollama:11434/v1");
assert.equal(aiBackendEnv.MULTICA_LLM_DEFAULT_MODEL, "${OLLAMA_MODEL:-qwen3:4b}");

const envExample = read(".env.local.example");
for (const variable of [
  "POSTGRES_PASSWORD",
  "JWT_SECRET",
  "MULTICA_VCS_SECRET_KEY",
  "FRONTEND_PORT",
  "BACKEND_PORT",
  "MAILPIT_UI_PORT",
  "OLLAMA_MODEL",
]) {
  assert.match(envExample, new RegExp(`^${variable}=`, "m"), `${variable} missing`);
}

const docs = read("LOCAL_DEPLOYMENT.md");
assert.match(docs, /scripts\/local-stack\.sh up/);
assert.match(docs, /scripts\\local-stack\.ps1 up/);
assert.match(docs, /docker-compose\.local-ai\.yml/);
assert.match(docs, /http:\/\/localhost:8025/);

const gitignore = read(".gitignore");
assert.match(gitignore, /^!\.env\.local\.example$/m);

console.log("local Docker stack configuration: ok");
