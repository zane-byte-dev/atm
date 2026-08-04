import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const injectedSessions = new Set<string>();

function sessionKey(ctx: ExtensionContext): string {
	return ctx.sessionManager.getSessionId() || ctx.cwd;
}

export default function (pi: ExtensionAPI) {
	pi.on("before_agent_start", async (_event, ctx) => {
		const sessionId = ctx.sessionManager.getSessionId();
		const key = sessionKey(ctx);
		if (injectedSessions.has(key)) return;
		injectedSessions.add(key);

		try {
			const args = [
				"todo",
				"match",
				"--prompt",
				"--limit",
				"3",
			];
			if (sessionId) args.push("--agent-session", sessionId);
			const { stdout } = await execFileAsync("/usr/local/bin/atm", args, {
				cwd: ctx.cwd,
				timeout: 10_000,
				maxBuffer: 16 * 1024,
			});
			const content = stdout.trim();
			if (!content) return;
			return {
				message: {
					customType: "atm-context",
					content,
					display: false,
					details: { source: "atm todo match --prompt" },
				},
			};
		} catch {
			// A transient ATM failure should not block Pi. Retry on the next turn.
			injectedSessions.delete(key);
		}
	});

	pi.on("session_shutdown", (_event, ctx) => {
		injectedSessions.delete(sessionKey(ctx));
	});
}
