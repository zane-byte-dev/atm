import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { connect } from "node:net";
import { homedir } from "node:os";
import { join } from "node:path";

/**
 * Reports Pi session activity to the ATM notch.
 *
 * Pi has no hooks config file, so this extension takes the place of the
 * `atm agent hook` bridge the Claude Code and Codex integrations use. It writes
 * the same envelope onto the same socket — see internal/agentevent/envelope.go
 * and ATMAgentEvent.swift; the three have to agree on these field names.
 *
 * Install: copy to ~/.pi/agent/extensions/atm-notch.ts
 */

const ENVELOPE_VERSION = 1;

type EventKind = "session_start" | "started" | "attention" | "completed" | "session_end";

function socketPath(): string {
	return process.env.ATM_NOTCH_SOCKET || join(homedir(), ".atm", "notch.sock");
}

/**
 * Sends one envelope, fire and forget.
 *
 * Every failure is swallowed on purpose: the ATM app is usually not running, and
 * a monitoring extension that throws inside an agent's event handler would break
 * the session it is supposed to be watching. Nothing here is awaited either, so a
 * wedged socket cannot stall a turn.
 */
function report(
	ctx: ExtensionContext,
	event: EventKind,
	extra: { reason?: string; tool?: string; text?: string } = {},
): void {
	try {
		const socket = connect(socketPath());
		socket.on("error", () => socket.destroy());
		socket.on("connect", () => {
			const envelope = {
				v: ENVELOPE_VERSION,
				source: "pi",
				event,
				session_id: ctx.sessionManager.getSessionId() || undefined,
				cwd: ctx.cwd,
				tool: extra.tool,
				reason: extra.reason,
				text: extra.text ? extra.text.slice(0, 400) : undefined,
				at: new Date().toISOString(),
			};
			socket.end(JSON.stringify(envelope) + "\n");
		});
		// Do not keep the process alive for a notification.
		socket.unref();
	} catch {
		// Nothing to do: the notch is optional.
	}
}

export default function (pi: ExtensionAPI) {
	pi.on("session_start", (_event, ctx) => {
		report(ctx, "session_start");
	});

	pi.on("input", (event, ctx) => {
		// The user handed work over, which also retires any pending attention
		// signal for this session.
		report(ctx, "started", { text: event.text });
	});

	pi.on("agent_settled", (_event, ctx) => {
		// Pi's own words: fired once the run has fully settled and no automatic
		// retry, compaction, or queued continuation will follow. That is exactly
		// the moment the ball is back with the user — unlike turn_end, which also
		// fires between the steps of a run that is still going.
		//
		// Pi does not distinguish "finished" from "blocked on a question", so this
		// reports attention when nothing is queued and completion otherwise. The
		// notch shows "等待下一步" rather than claiming to know which it was.
		if (ctx.hasPendingMessages()) {
			report(ctx, "completed");
			return;
		}
		report(ctx, "attention", { reason: "settled" });
	});

	pi.on("session_shutdown", (_event, ctx) => {
		report(ctx, "session_end");
	});
}
