// Runs the real stack in a disposable schema of the explicitly supplied test DB.
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdir } from "node:fs/promises";
import { createServer } from "node:http";
import { fileURLToPath } from "node:url";
import path from "node:path";
import pg from "pg";

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const root = path.resolve(web, "../..");
const raw = process.env.TEST_DATABASE_URL;
if (!raw || !/test/i.test(new URL(raw).pathname) || raw === process.env.DATABASE_URL) {
  throw new Error("Set TEST_DATABASE_URL to a dedicated test database (name must contain 'test'). Never use the application database.");
}
const schema = `t7_${randomBytes(8).toString("hex")}`;
const db = new pg.Pool({ connectionString: raw, connectionTimeoutMillis: 5000 });
db.on("error", () => {}); // Idle connections can close during the intentional restart.
const scoped = new URL(raw);
scoped.searchParams.set("search_path", schema);
const controlKey = randomBytes(24).toString("hex");
const env = { ...process.env, DATABASE_URL: scoped.href, HOST: "127.0.0.1", PORT: "18080",
  ALLOWED_ORIGINS: "http://127.0.0.1:13000", COOKIE_SECURE: "false",
  NEXT_PUBLIC_API_URL: "http://127.0.0.1:18080", NEXT_PUBLIC_WS_URL: "ws://127.0.0.1:18080/ws",
  E2E_DATABASE_URL: scoped.href, E2E_CONTROL_KEY: controlKey,
};
const binary = path.join(root, "bin", process.platform === "win32" ? "t7-api.exe" : "t7-api");
let api, frontend, control;
const start = (command, args, cwd = web) => spawn(command, args, { cwd, env, stdio: "inherit", windowsHide: true });
const run = (command, args, cwd) => new Promise((resolve, reject) => {
  const child = start(command, args, cwd);
  child.once("error", reject);
  child.once("exit", code => code === 0 ? resolve() : reject(new Error(`${path.basename(command)} exited with ${code}`)));
});
async function stop(child) {
  if (!child || child.exitCode !== null) return;
  await new Promise(resolve => { child.once("exit", resolve); child.kill(); });
}
async function ready(url) {
  for (let attempt = 0; attempt < 60; attempt++) {
    try { if ((await fetch(url, { signal: AbortSignal.timeout(1000) })).ok) return; } catch {}
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  throw new Error(`Server did not become ready: ${url}`);
}
async function freePort(port) {
  const { createServer } = await import("node:net");
  await new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", () => reject(new Error(`Test port ${port} is in use; stop its process first.`)));
    server.listen(port, "127.0.0.1", () => server.close(resolve));
  });
}
try {
  await Promise.all([freePort(13000), freePort(18080), freePort(18081)]);
  await db.query(`CREATE SCHEMA ${schema}`);
  console.log(`Created isolated E2E schema ${schema}`);
  await mkdir(path.dirname(binary), { recursive: true });
  await run("go", ["build", "-o", binary, "./cmd/api"], path.join(root, "apps/api"));
  await run(process.execPath, ["node_modules/next/dist/bin/next", "build"]);
  api = start(binary, [], root);
  await ready("http://127.0.0.1:18080/health");
  frontend = start(process.execPath, ["node_modules/next/dist/bin/next", "start", "--hostname", "127.0.0.1", "--port", "13000"]);
  await ready("http://127.0.0.1:13000/signin");
  // Test-only control server, loopback + random capability; never part of the app.
  control = createServer(async (req, res) => {
    if (req.method !== "POST" || req.headers.authorization !== `Bearer ${controlKey}` || req.url !== "/restart") { res.writeHead(403).end(); return; }
    try {
      await stop(api);
      if (process.env.E2E_RESTART_POSTGRES === "1") {
        await run("docker", ["compose", "restart", "postgres"], root);
      }
      // pg_isready is not required on the host. Wait using the test role.
      for (let attempt = 0; ; attempt++) {
        try { await db.query("SELECT 1"); break; }
        catch { if (attempt >= 30) throw new Error("Database did not recover"); await new Promise(resolve => setTimeout(resolve, 500)); }
      }
      api = start(binary, [], root);
      await ready("http://127.0.0.1:18080/health");
      res.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ postgresRestarted: process.env.E2E_RESTART_POSTGRES === "1" }));
    } catch { res.writeHead(500).end("Restart failed"); }
  });
  await new Promise(resolve => control.listen(18081, "127.0.0.1", resolve));
  await run(process.execPath, ["node_modules/@playwright/test/cli.js", "test", ...process.argv.slice(2)]);
} catch (error) {
  // Do not print driver errors that could include connection credentials.
  console.error(error instanceof Error && !error.message.includes(raw) ? error.message : "E2E failed; check the test database and server configuration.");
  process.exitCode = 1;
} finally {
  if (control) await new Promise(resolve => control.close(resolve));
  await stop(frontend); await stop(api);
  await db.query(`DROP SCHEMA IF EXISTS ${schema} CASCADE`).catch(() => { console.error(`Cleanup failed for ${schema}`); process.exitCode = 1; });
  await db.end();
  console.log(`E2E processes stopped; cleanup attempted for ${schema}.`);
}
