import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { setTimeout as delay } from "node:timers/promises";

const supervisor = resolve("scripts/dev.mjs");
async function until(check) {
  for (let n = 0; n < 150; n++) {
    if (await check()) return;
    await delay(100);
  }
  throw new Error("Timed out");
}

test(
  "takeover, failed build retention, recovery, source filtering and shutdown",
  { timeout: 45000 },
  async () => {
    const dir = await mkdtemp(join(tmpdir(), "atm-dev-test-"));
    const ui = createServer((req, res) => res.end("vite"));
    await new Promise((done) => ui.listen(0, "127.0.0.1", done));
    const portProbe = createServer();
    await new Promise((done) => portProbe.listen(0, "127.0.0.1", done));
    const apiPort = portProbe.address().port;
    await new Promise((done) => portProbe.close(done));
    let runner, original;
    try {
      for (const path of [
        "cmd",
        "internal",
        "scripts",
        "fakebin",
        "app/web/src",
      ])
        await mkdir(join(dir, path), { recursive: true });
      await writeFile(
        join(dir, "scripts/codesign-local.sh"),
        "#!/bin/sh\nexit 0\n",
        { mode: 0o755 },
      );
      const fixture = `#!/usr/bin/env node
const fs = require('fs'), http = require('http');
const args = process.argv.slice(2), pidFile = 'server.pid';
if (args[1] === 'status') {
 let pid; try { pid = +fs.readFileSync(pidFile); process.kill(pid, 0); } catch { pid = 0; }
 console.log(JSON.stringify({running: !!pid, instance: {origin: 'local-test'}}));
} else if (args[1] === 'stop') {
 process.kill(+fs.readFileSync(pidFile), 'SIGTERM');
} else {
 const server = http.createServer((req,res) => res.end('ok'));
 server.listen(+args[args.indexOf('--port')+1], '127.0.0.1', () => {
  fs.writeFileSync(pidFile, String(process.pid));
  fs.appendFileSync('starts', process.pid + '\\n');
 });
 process.on('SIGTERM', () => { fs.rmSync(pidFile, {force:true}); server.close(() => process.exit()); });
}
`;
      await writeFile(join(dir, "fixture.cjs"), fixture, { mode: 0o755 });
      await writeFile(
        join(dir, "fakebin/go"),
        `#!/usr/bin/env node
const fs = require('fs');
fs.appendFileSync('builds','build\\n');
if (fs.existsSync('fail')) process.exit(1);
const output = process.argv[process.argv.indexOf('-o')+1];
fs.copyFileSync('fixture.cjs',output); fs.chmodSync(output,0o755);
`,
        { mode: 0o755 },
      );
      const cwdEnv = {
        ...process.env,
        PATH: `${join(dir, "fakebin")}:${process.env.PATH}`,
      };
      original = spawn(
        process.execPath,
        ["fixture.cjs", "serve", "--port", String(apiPort)],
        { cwd: dir, env: cwdEnv },
      );
      const pid = async () => {
        try {
          return +(await readFile(join(dir, "server.pid"), "utf8"));
        } catch {
          return 0;
        }
      };
      await until(async () => (await pid()) === original.pid);
      runner = spawn(process.execPath, [supervisor, "api"], {
        cwd: dir,
        env: {
          ...cwdEnv,
          DEV_BIN_DIR: "bin",
          DEV_DATA_DIR: dir,
          DEV_PORT: String(apiPort),
          DEV_OPEN: "",
          DEV_UI_URL: `http://127.0.0.1:${ui.address().port}`,
        },
        stdio: ["ignore", "pipe", "pipe"],
      });
      let logs = "";
      runner.stdout.on("data", (data) => {
        logs += data;
      });
      runner.stderr.on("data", (data) => {
        logs += data;
      });
      await until(
        async () => (await pid()) > 0 && (await pid()) !== original.pid,
      );
      const first = await pid();
      await writeFile(join(dir, "fail"), "");
      await writeFile(join(dir, "internal/change.go"), "broken");
      await until(() => logs.includes("keeping the previous backend"));
      assert.equal(
        await pid(),
        first,
        "failed compilation must preserve the running backend",
      );
      await rm(join(dir, "fail"));
      await writeFile(join(dir, "internal/change.go"), "fixed");
      await until(async () => (await pid()) > 0 && (await pid()) !== first);
      const second = await pid();
      const buildCount = await readFile(join(dir, "builds"), "utf8");
      await writeFile(join(dir, "app/web/src/style.css"), "body {}");
      await delay(1200);
      assert.equal(
        await readFile(join(dir, "builds"), "utf8"),
        buildCount,
        "CSS must not rebuild Go",
      );
      const exited = new Promise((done) => runner.once("exit", done));
      runner.kill("SIGTERM");
      await exited;
      assert.equal(await pid(), 0);
      assert.throws(() => process.kill(second, 0), { code: "ESRCH" });
    } finally {
      runner?.kill("SIGTERM");
      original?.kill("SIGTERM");
      await new Promise((done) => ui.close(done));
      await rm(dir, { recursive: true, force: true });
    }
  },
);
