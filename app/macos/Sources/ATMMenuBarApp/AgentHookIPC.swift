import Foundation

/// Typed hook registration for the settings pane.
///
/// This used to be three ordinary `agent hook <verb> --json` argv calls. They
/// were the last writes the App made by argv, which meant the only thing standing
/// between a renamed flag and a settings pane that silently stopped installing
/// hooks was a text file listing the arrays. Under `_ipc` they move with the rest
/// of the protocol version instead.
///
/// `.useDefault` because ATMAgentHookReport and ATMAgentHookSource already spell
/// their keys out in CodingKeys; converting on top of that would look for
/// `socketPath` and find nothing.
enum ATMAgentHookIPCCommand {
    static let install = ATMIPCMethod<ATMAgentHookRequest, ATMAgentHookReport>(
        "agent.hook.install",
        responseKeyDecoding: .useDefault
    )
    static let status = ATMIPCMethod<ATMAgentHookRequest, ATMAgentHookReport>(
        "agent.hook.status",
        responseKeyDecoding: .useDefault
    )
    static let uninstall = ATMIPCMethod<ATMAgentHookRequest, ATMAgentHookReport>(
        "agent.hook.uninstall",
        responseKeyDecoding: .useDefault
    )
}

/// Narrows registration to one agent. A nil source asks for every agent ATM
/// knows how to wire up, which is what all three buttons in the settings pane do
/// — they act on the fleet, not on one row.
struct ATMAgentHookRequest: Encodable, Equatable {
    let source: String?

    init(source: String? = nil) {
        self.source = source
    }
}

struct ATMAgentHookIPCClient: Sendable {
    private let ipc: ATMIPCClient

    init(runner: ATMCommandRunner) {
        ipc = ATMIPCClient(runner: runner)
    }

    init() throws {
        ipc = try ATMIPCClient()
    }

    func status(_ request: ATMAgentHookRequest = ATMAgentHookRequest()) async throws -> ATMAgentHookReport {
        try await ipc.call(ATMAgentHookIPCCommand.status, request: request)
    }

    func install(_ request: ATMAgentHookRequest = ATMAgentHookRequest()) async throws -> ATMAgentHookReport {
        try await ipc.call(ATMAgentHookIPCCommand.install, request: request)
    }

    func uninstall(_ request: ATMAgentHookRequest = ATMAgentHookRequest()) async throws -> ATMAgentHookReport {
        try await ipc.call(ATMAgentHookIPCCommand.uninstall, request: request)
    }
}
