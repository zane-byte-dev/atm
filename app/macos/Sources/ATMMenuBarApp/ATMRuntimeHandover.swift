import AppKit
import Darwin

final class ATMLegacyRuntimeLease {
    private let descriptor: Int32
    init(directory: URL) throws {
        let runtime = directory.appendingPathComponent("runtime", isDirectory: true)
        try FileManager.default.createDirectory(at: runtime, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let fd = open(runtime.appendingPathComponent("presence.lock").path, O_RDWR | O_CREAT | O_NOFOLLOW, 0o600)
        guard fd >= 0 else { throw POSIXError(.EACCES) }
        guard flock(fd, LOCK_EX | LOCK_NB) == 0 else { close(fd); throw POSIXError(.EBUSY) }
        descriptor = fd
    }
    deinit { flock(descriptor, LOCK_UN); close(descriptor) }
}

enum ATMRuntimeHandover {
    enum Mode: Equatable { case legacy, web, voiceOnly }
    static var dataDirectory: URL {
        if let path = ProcessInfo.processInfo.environment["ATM_DATA_DIR"], !path.isEmpty { return URL(fileURLWithPath: path) }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".atm", isDirectory: true)
    }
    static var mode: Mode {
        resolve(environment: ProcessInfo.processInfo.environment, defaults: UserDefaults.standard,
                marker: dataDirectory.appendingPathComponent("runtime/presence-owner.json"))
    }
    static func resolve(environment: [String: String], defaults: UserDefaults, marker: URL) -> Mode {
        if environment["ATM_VOICE_ONLY"] == "1" { return .voiceOnly }
        if defaults.string(forKey: "ATMRuntimeOwner") == "go" { return .web }
        guard let data = try? Data(contentsOf: marker),
              let value = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              value["owner"] as? String == "go" else { return .legacy }
        // Ownership is a deliberate migration choice, not an expiring liveness
        // check. A crashed/stopped Go process must never resurrect legacy jobs.
        return .web
    }
}

@MainActor
final class ATMLegacyTransitionController: NSObject {
    private let mode: ATMRuntimeHandover.Mode
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    init(mode: ATMRuntimeHandover.Mode) {
        self.mode = mode
        super.init()
        statusItem.button?.image = NSImage(systemSymbolName: mode == .voiceOnly ? "waveform" : "globe", accessibilityDescription: "ATM 已迁移")
        let menu = NSMenu()
        let label = menu.addItem(withTitle: mode == .voiceOnly ? "旧 ATM · 仅语音过渡模式" : "ATM 已由 Go 服务接管", action: nil, keyEquivalent: "")
        label.isEnabled = false
        let item = menu.addItem(withTitle: "打开 Web 工作区", action: #selector(openWeb), keyEquivalent: "")
        item.target = self
        menu.addItem(.separator())
        menu.addItem(withTitle: "退出旧 ATM", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        statusItem.menu = menu
        if mode == .voiceOnly {
            let keys = ATMGlobalHotKeyManager.shared
            keys.onPressed = { action in
                if action == .voiceInput { ATMVoiceInputCoordinator.shared.hotKeyPressed() }
                if action == .cancelVoice { ATMVoiceInputCoordinator.shared.cancel() }
            }
            keys.onReleased = { action in if action == .voiceInput { ATMVoiceInputCoordinator.shared.hotKeyReleased() } }
            keys.start(allowedActions: [.voiceInput, .cancelVoice])
        }
    }
    func stop() {
        if mode == .voiceOnly { ATMVoiceInputCoordinator.shared.cancel(); ATMGlobalHotKeyManager.shared.stop() }
        NSStatusBar.system.removeStatusItem(statusItem)
    }
    @objc func openWeb() {
        Task { @MainActor in
            do {
                let runtime = ATMRuntimeHandover.dataDirectory.appendingPathComponent("runtime")
                let data = try Data(contentsOf: runtime.appendingPathComponent("server.json"))
                let instance = try JSONDecoder().decode(Instance.self, from: data)
                guard let origin = URL(string: instance.origin), origin.scheme == "http", origin.host == "127.0.0.1", origin.user == nil, origin.password == nil,
                      origin.query == nil, origin.fragment == nil, origin.path.isEmpty || origin.path == "/" else { throw URLError(.badURL) }
                let token = try String(contentsOf: runtime.appendingPathComponent("control.token"), encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)
                var request = URLRequest(url: origin.appendingPathComponent("api/v1/control/open"), timeoutInterval: 5)
                request.httpMethod = "POST"
                request.httpBody = Data("{}".utf8)
                request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
                request.setValue(instance.instanceID, forHTTPHeaderField: "X-ATM-Instance")
                let session = URLSession(configuration: .ephemeral, delegate: NoRedirect(), delegateQueue: nil)
                defer { session.invalidateAndCancel() }
                let (response, _) = try await session.data(for: request)
                let result = try JSONDecoder().decode(OpenReply.self, from: response)
                guard let url = URL(string: result.data.url), url.scheme == origin.scheme, url.host == origin.host, url.port == origin.port else { throw URLError(.badURL) }
                NSWorkspace.shared.open(url)
            } catch {
                let alert = NSAlert()
                alert.messageText = "Web 服务尚未就绪"
                alert.informativeText = "请运行 atm serve --open。旧 App 已停用任务存储、自动同步、采集和 Hook 接收。"
                alert.runModal()
            }
        }
    }
    private struct Instance: Decodable {
        let origin: String
        let instanceID: String
        enum CodingKeys: String, CodingKey { case origin; case instanceID = "instance_id" }
    }
    private struct OpenReply: Decodable { struct Payload: Decodable { let url: String }; let data: Payload }
    private final class NoRedirect: NSObject, URLSessionTaskDelegate {
        func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) { completionHandler(nil) }
    }
}
