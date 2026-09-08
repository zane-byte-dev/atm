// Local development supervisor. Node is already required by Vite; no watcher dependency.
import { spawn } from "node:child_process";
import { mkdir, readdir, stat, rename } from "node:fs/promises";
import { resolve, join } from "node:path";
import { createInterface } from "node:readline";
import { setTimeout as delay } from "node:timers/promises";

const mode = process.argv[2] || "all";
if (!["all", "ui", "api"].includes(mode))
  throw new Error(`Unknown dev mode: ${mode}`);
const env = process.env;
const apiPort = Number(env.DEV_PORT || "47321");
if (!Number.isInteger(apiPort) || apiPort < 1 || apiPort > 65535)
  throw new Error("DEV_PORT must be between 1 and 65535");
const binDir = resolve(env.DEV_BIN_DIR || "bin/atm-web-dev");
const binary = join(binDir, "atm");
const candidate = join(binDir, "next", "atm");
const dataArgs = env.DEV_DATA_DIR ? ["--data-dir", env.DEV_DATA_DIR] : [];
const uiURL = env.DEV_UI_URL || "http://127.0.0.1:5173";
const children = new Set();
let stopping = false;
let failure;
let backend;
let frontend;
const log = (message) => console.log(`[dev] ${message}`);

function launch(command, args, label, capture = false) {
  const child = spawn(command, args, {
    detached: true,
    stdio: ["ignore", "pipe", "pipe"],
  });
  children.add(child);
  let output = "";
  for (const [stream, isError] of [
    [child.stdout, false],
    [child.stderr, true],
  ]) {
    createInterface({ input: stream }).on("line", (line) => {
      if (capture && !isError) output += line + "\n";
      else console.log(`[${label}] ${line}`);
    });
  }
  child.finished = new Promise((done) => {
    child.once("error", (error) => {
      log(error.message);
      done(1);
    });
    child.once("close", (code) => {
      children.delete(child);
      done(code ?? 1);
    });
  });
  child.output = () => output;
  return child;
}
async function run(command, args, label, capture = false) {
  if (stopping) throw new Error("Development stopped");
  const child = launch(command, args, label, capture);
  const code = await child.finished;
  if (code !== 0) throw new Error(`${label} exited with code ${code}`);
  return child.output();
}
async function stop(child) {
  if (!child || !children.has(child)) return;
  child.expectedExit = true;
  try {
    process.kill(-child.pid, "SIGTERM");
  } catch (error) {
    if (error.code !== "ESRCH") throw error;
  }
  const timer = setTimeout(() => {
    try {
      process.kill(-child.pid, "SIGKILL");
    } catch {}
  }, 10000);
  try {
    await child.finished;
  } finally {
    clearTimeout(timer);
  }
}
function monitor(child, name) {
  child.finished.then(() => {
    if (!stopping && !child.expectedExit) {
      failure = new Error(`${name} stopped unexpectedly; check the log above`);
      stopping = true;
    }
  });
}
for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, () => {
    stopping = true;
    for (const child of children) void stop(child);
  });

// Poll only Go inputs, not generated binaries, UI files or the live data directory.
async function fingerprint() {
  const inputs = [];
  async function visit(path) {
    try {
      const info = await stat(path);
      if (info.isDirectory()) {
        for (const name of (await readdir(path)).sort())
          await visit(join(path, name));
      } else inputs.push(`${path}:${info.mtimeMs}:${info.size}`);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
  }
  for (const path of [
    "cmd",
    "internal",
    "go.mod",
    "go.sum",
    "app/web/assets_stub.go",
    "app/web/assets_embed.go",
    "scripts/codesign-local.sh",
  ])
    await visit(path);
  return inputs.join("\n");
}
async function ready(url) {
  for (let attempt = 0; attempt < 100 && !stopping; attempt++) {
    try {
      if ((await fetch(url, { signal: AbortSignal.timeout(500) })).ok) return;
    } catch {}
    await delay(100);
  }
  throw new Error(`Not ready: ${url}`);
}
async function build() {
  log(
    "Building Go; the running backend stays available until compilation succeeds.",
  );
  await mkdir(join(binDir, "next"), { recursive: true });
  await run(
    "go",
    [
      "build",
      "-trimpath",
      "-ldflags",
      env.DEV_LDFLAGS || "-X main.version=dev",
      "-o",
      candidate,
      "./cmd/atm",
    ],
    "build",
  );
  if (process.platform === "darwin")
    await run("./scripts/codesign-local.sh", [candidate], "sign");
}
async function replaceBackend(first) {
  // The first takeover uses ATM's existing managed-service shutdown path.
  // Later restarts stop only the child owned by this supervisor.
  if (backend) await stop(backend);
  else {
    const status = JSON.parse(
      await run(
        candidate,
        ["serve", "status", ...dataArgs, "--json"],
        "api",
        true,
      ),
    );
    if (status.running) {
      log(`Taking over the local workspace at ${status.instance.origin}`);
      await run(candidate, ["serve", "stop", ...dataArgs], "api");
      let stopped = false;
      for (let i = 0; i < 100 && !stopping; i++) {
        const current = JSON.parse(
          await run(
            candidate,
            ["serve", "status", ...dataArgs, "--json"],
            "api",
            true,
          ),
        );
        if (!current.running) {
          stopped = true;
          break;
        }
        await delay(100);
      }
      if (!stopped)
        throw new Error(
          "Existing workspace did not stop; refusing to start another backend",
        );
    }
  }
  if (stopping) return;
  await rename(candidate, binary);
  backend = launch(
    binary,
    [
      "serve",
      ...dataArgs,
      "--port",
      String(apiPort),
      "--dev-ui",
      uiURL,
      ...(first && env.DEV_OPEN !== "" ? ["--open"] : []),
    ],
    "api",
  );
  monitor(backend, "Backend");
  await ready(`http://127.0.0.1:${String(apiPort)}/healthz`);
  log(
    "Backend ready. Go changes rebuild automatically; React/CSS changes use Vite HMR.",
  );
}

try {
  if (mode !== "api") {
    try {
      await stat("app/web/node_modules/.bin/vite");
    } catch {
      await run(env.NPM || "npm", ["ci", "--prefix", "app/web"], "ui");
    }
    frontend = launch(
      env.NPM || "npm",
      [
        "run",
        "dev",
        "--prefix",
        "app/web",
        "--",
        "--port",
        env.DEV_UI_PORT || "5173",
      ],
      "ui",
    );
    monitor(frontend, "Vite");
    await ready(uiURL);
  }
  if (mode === "ui") {
    log(
      "Frontend ready. Open the Go workspace address for API and authentication.",
    );
    while (!stopping) await delay(200);
  } else {
    let previous;
    let started = false;
    log(
      `Data: ${env.DEV_DATA_DIR || "configured local ATM directory (same as daily CLI)"}`,
    );
    while (!stopping) {
      const current = await fingerprint();
      if (current !== previous) {
        // Debounce saves. Record the pre-build fingerprint so edits during a build are not lost.
        await delay(250);
        previous = await fingerprint();
        try {
          await build();
        } catch (error) {
          if (!stopping)
            log(
              `${error.message}; keeping the previous backend. Save a Go file to retry.`,
            );
          continue;
        }
        if (!stopping) {
          await ready(uiURL);
          await replaceBackend(!started);
          started = true;
        }
      }
      await delay(500);
    }
  }
} catch (error) {
  if (!stopping) failure = error;
} finally {
  stopping = true;
  await Promise.all([...children].map(stop));
  if (failure) {
    console.error(`[dev] ${failure.message}`);
    process.exitCode = 1;
  }
}
