import AppKit
import SwiftUI
import XCTest
@testable import ATMMenuBarApp

final class TodoLinksTests: XCTestCase {
    func testGroupsAndCustomKindsPreserveExistingMetadata() {
        for (kind, group) in [("cr", ATMTodoLinkGroup.review), ("mr", .review), ("pr", .review),
                              ("pipeline", .delivery), ("release", .delivery), ("preview", .delivery),
                              ("website", .delivery), ("document", .documents), ("artifact", .documents),
                              ("workitem", .documents), ("custom", .other)] {
            let link = ATMTodoLink(url: "https://example.com", kind: kind, title: "", relation: nil)
            XCTAssertEqual(link.group, group)
            XCTAssertFalse(link.kindLabel.isEmpty)
            XCTAssertEqual(link.displayTitle, link.url)
        }
        let custom = ATMTodoLink(url: "https://example.com/cr/1", kind: "custom", title: "  Design  ", relation: "custom purpose")
        XCTAssertEqual(custom.effectiveKind, "custom")
        XCTAssertEqual(custom.displayTitle, "Design")
        XCTAssertEqual(custom.relationLabel, "custom purpose")
        XCTAssertEqual(ATMTodoLink(url: custom.url, kind: "other", title: nil, relation: nil).group, .other)
    }

    func testInferenceMatchesWorkRulesAndRespectsExplicitType() {
        for (url, kind) in [
            ("https://code.alibaba-inc.com/a/b/codereview/1", "mr"),
            ("https://github.com/a/b/pull/1", "mr"), ("https://example.com/cr/1", "cr"),
            ("https://example.com/pipelines/1", "pipeline"), ("https://example.com/releases/1", "release"),
            ("https://example.com/issues/1", "workitem"), ("https://www.yuque.com/a/b", "document"),
            ("https://alidocs.dingtalk.com/i/nodes/1", "document"), ("https://example.com/REPORT.PDF", "document"),
            ("https://yuque.com.evil.test/a", ""), ("https://example.com", "")
        ] {
            XCTAssertEqual(ATMTodoLink.inferredKind(for: url), kind, url)
            XCTAssertEqual(ATMTodoLink(url: url, kind: nil, title: nil, relation: nil).effectiveKind, kind)
        }
    }

    func testOnlyWebDestinationsAreOpenable() {
        for url in ["javascript:alert(1)", "file:///etc/passwd", "codex://threads/1", "example.com", "https://user:pass@example.com"] {
            XCTAssertNil(ATMTodoLink(url: url, kind: nil, title: nil, relation: nil).destination, url)
        }
        XCTAssertNotNil(ATMTodoLink(url: "https://example.com/path", kind: nil, title: nil, relation: nil).destination)
    }

    func testTypedRequestsPreserveExplicitClearsAndOriginalURL() throws {
        XCTAssertEqual(ATMTodoIPCCommand.saveLink.arguments, ["_ipc", "todo.link.save"])
        XCTAssertEqual(ATMTodoIPCCommand.removeLink.arguments, ["_ipc", "todo.link.remove"])
        let save = ATMTodoLinkSaveRequest(todoID: "t1", originalURL: "https://example.com/old", url: "https://example.com/new", kind: "preview", title: "", relation: "")
        let data = try JSONEncoder().encode(save)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(object["todo_id"] as? String, "t1")
        XCTAssertEqual(object["original_url"] as? String, "https://example.com/old")
        XCTAssertEqual(object["title"] as? String, "")
        XCTAssertEqual(object["relation"] as? String, "")
        XCTAssertEqual(Set(object.keys), ["todo_id", "original_url", "url", "kind", "title", "relation"])
        let add = ATMTodoLinkSaveRequest(todoID: "t1", originalURL: nil, url: "https://example.com", kind: "", title: "", relation: "")
        let added = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(add)) as? [String: Any])
        XCTAssertNil(added["original_url"])
    }

    private func client(verb: String, payload: String) throws -> (ATMTodoIPCClient, URL, URL) {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("atm-links-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let script = directory.appendingPathComponent("atm")
        let request = directory.appendingPathComponent("request.json")
        func quote(_ text: String) -> String { "'" + text.replacingOccurrences(of: "'", with: "'\"'\"'") + "'" }
        let response = "{\"envelope_version\":1,\"protocol_version\":1,\"request_id\":\"test\",\"verb\":\"\(verb)\",\(payload)}"
        try "#!/bin/sh\n/bin/cat > \(quote(request.path))\n/usr/bin/printf '%s' \(quote(response))\n"
            .write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
        return (ATMTodoIPCClient(runner: try ATMCommandRunner(environment: ["ATM_EXECUTABLE": script.path])), directory, request)
    }

    func testSaveClientUsesTypedResponse() async throws {
        let (client, directory, request) = try client(verb: "todo.link.save", payload: #"""
        "data":{"id":"t1","title":"Task","priority":"P2","status":"open","created":"2026-09-03","links":[{"url":"https://example.com","kind":"website","title":"Online"}]}
        """#)
        defer { try? FileManager.default.removeItem(at: directory) }
        let result = try await client.saveLink(ATMTodoLinkSaveRequest(todoID: "t1", originalURL: nil, url: "https://example.com", kind: "website", title: "Online", relation: ""))
        XCTAssertEqual(result.links?.first?.kind, "website")
        XCTAssertEqual(result.status, "open")
        let body = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(contentsOf: request)) as? [String: Any])
        XCTAssertEqual(body["todo_id"] as? String, "t1")
        XCTAssertEqual(body["url"] as? String, "https://example.com")
    }

    func testRemoveClientDecodesEmptyLinks() async throws {
        let (client, directory, request) = try client(verb: "todo.link.remove", payload: #"""
        "data":{"id":"t1","title":"Task","priority":"P2","status":"open","created":"2026-09-03"}
        """#)
        defer { try? FileManager.default.removeItem(at: directory) }
        let result = try await client.removeLink(ATMTodoLinkRemoveRequest(todoID: "t1", url: "https://example.com"))
        XCTAssertTrue(result.links?.isEmpty ?? true)
        let body = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(contentsOf: request)) as? [String: Any])
        XCTAssertEqual(Set(body.keys), ["todo_id", "url"])
    }

    func testSaveFailurePropagatesWithoutSuccess() async throws {
        let (client, directory, _) = try client(verb: "todo.link.save", payload: #"""
        "error":{"code":"conflict","message":"该地址已关联","retryable":false}
        """#)
        defer { try? FileManager.default.removeItem(at: directory) }
        do {
            _ = try await client.saveLink(ATMTodoLinkSaveRequest(todoID: "t1", originalURL: nil, url: "https://example.com", kind: "", title: "", relation: ""))
            XCTFail("Expected conflict")
        } catch {
            XCTAssertTrue(error.localizedDescription.contains("该地址已关联"))
        }
    }

    @MainActor
    func testRelatedContentRenders() throws {
        let todo = try JSONDecoder().decode(ATMTodo.self, from: Data(#"""
        {"id":"t1","title":"发布关联内容功能","priority":"P2","status":"open","created":"2026-09-03","links":[
          {"url":"https://code.example.com/atm/merge_requests/42","kind":"mr","title":"MR #42 · 关联内容页","relation":"代码评审"},
          {"url":"https://deploy.example.com/release/108","kind":"release","title":"发布单 #108","relation":"正式环境发布记录"},
          {"url":"https://atm.example.com","kind":"website","title":"线上访问地址"},
          {"url":"https://docs.example.com/docs/design","kind":"document","title":"设计方案与验收清单","relation":"evidence"}
        ]}
        """#.utf8))
        let view = NSHostingView(rootView:
            DesktopTodoLinksView(todo: todo, store: ATMDataStore(), isArchived: false)
                .frame(width: 600, height: 700, alignment: .topLeading)
                .background(Color(nsColor: .windowBackgroundColor))
                .environment(\.colorScheme, .light)
        )
        // ImageRenderer cannot render native Link/Menu controls; use AppKit's
        // actual hosting view so visual QA includes those controls as well.
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 600, height: 700), styleMask: [.borderless], backing: .buffered, defer: false)
        window.contentView = view
        view.frame = window.contentLayoutRect
        view.layoutSubtreeIfNeeded()
        let bitmap = try XCTUnwrap(view.bitmapImageRepForCachingDisplay(in: view.bounds))
        view.cacheDisplay(in: view.bounds, to: bitmap)
        XCTAssertGreaterThanOrEqual(bitmap.pixelsWide, 600)
        let png = try XCTUnwrap(bitmap.representation(using: .png, properties: [:]))
        let path = FileManager.default.temporaryDirectory.appendingPathComponent("atm-related-content-preview.png")
        try png.write(to: path)
        print("Related content preview: \(path.path)")
    }
}
