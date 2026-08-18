import SwiftUI
import UniformTypeIdentifiers

/// A small, ID-only order persisted in `UserDefaults`.
///
/// The underlying catalog remains authoritative for which objects exist. Saved
/// IDs only choose their presentation order: deleted IDs disappear and newly
/// discovered IDs are appended in the caller's fallback order.
enum ATMManualOrder {
    static let knowledgeCollectionsKey = "ATMKnowledgeCollectionOrder"
    static let collectionSourcesKey = "ATMCollectionSourceOrder"

    /// Kept as `public.utf8-plain-text` via the plain `NSItemProvider(object:)`
    /// initializer, which is the construction the drag is known to work with.
    ///
    /// A private exported type would be the better answer — it would stop a text
    /// drag from another app from ever becoming a drop candidate here, which is
    /// the one hole this design still has (see `pendingMoveSource`). But whether
    /// such a type survives SwiftUI's macOS `onDrag` bridge is not something
    /// this repo can test: `NSItemProvider` is not `NSPasteboardWriting`, so the
    /// pasteboard hand-off happens inside SwiftUI where no test can reach it,
    /// and a wrong guess costs the whole reordering gesture. Left alone until a
    /// manual drag can confirm it.
    static func itemProvider(for id: String) -> NSItemProvider {
        NSItemProvider(object: id as NSString)
    }

    static func ordered<Element>(
        _ values: [Element],
        stored rawValue: String,
        id: (Element) -> String
    ) -> [Element] {
        // `uniquingKeysWith` rather than `uniqueKeysWithValues`: the latter traps
        // on duplicate IDs, and a helper that only picks a display order must not
        // be able to take the app down over one malformed catalog payload.
        let valuesByID = Dictionary(values.map { (id($0), $0) }, uniquingKeysWith: { first, _ in first })
        return reconciledIDs(stored: rawValue, fallback: values.map(id)).compactMap { valuesByID[$0] }
    }

    static func reconciledIDs(stored rawValue: String, fallback: [String]) -> [String] {
        let available = Set(fallback)
        var seen = Set<String>()
        var result = decode(rawValue).filter { available.contains($0) && seen.insert($0).inserted }
        result.append(contentsOf: fallback.filter { seen.insert($0).inserted })
        return result
    }

    static func moving(
        _ draggedID: String,
        over targetID: String,
        stored rawValue: String,
        fallback: [String]
    ) -> String {
        var ids = reconciledIDs(stored: rawValue, fallback: fallback)
        // Returning `rawValue` rather than the reconciled order matters: a drag
        // whose row vanished mid-gesture (the catalog refreshes on a timer) lands
        // here, and persisting the fallback order would freeze a sort the user
        // never chose — knowledge libraries would stop re-sorting on rename.
        guard let sourceIndex = ids.firstIndex(of: draggedID),
              let targetIndex = ids.firstIndex(of: targetID),
              sourceIndex != targetIndex else {
            return rawValue
        }
        ids.remove(at: sourceIndex)
        ids.insert(draggedID, at: targetIndex)
        return encode(ids)
    }

    /// The keyboard- and menu-reachable half of reordering: dragging is
    /// mouse-only, and rows scrolled out of view are not drop targets.
    @ATMMenuBuilder
    static func moveMenuEntries(
        for id: String,
        in ids: [String],
        move: @escaping (_ draggedID: String, _ targetID: String) -> Void
    ) -> [ATMMenuEntry] {
        let index = ids.firstIndex(of: id)
        ATMMenuItem("上移", systemImage: "arrow.up", enabled: index.map { $0 > 0 } ?? false) {
            guard let index, index > 0 else { return }
            move(id, ids[index - 1])
        }
        ATMMenuItem("下移", systemImage: "arrow.down", enabled: index.map { $0 + 1 < ids.count } ?? false) {
            guard let index, index + 1 < ids.count else { return }
            move(id, ids[index + 1])
        }
    }

    static func encode(_ ids: [String]) -> String {
        guard let data = try? JSONEncoder().encode(ids) else { return "[]" }
        return String(decoding: data, as: UTF8.self)
    }

    static func decode(_ rawValue: String) -> [String] {
        guard let data = rawValue.data(using: .utf8),
              let ids = try? JSONDecoder().decode([String].self, from: data) else {
            return []
        }
        return ids
    }
}

/// Reorders as the pointer crosses rows, which gives immediate feedback and
/// keeps `performDrop` responsible only for ending the drag session.
struct ATMManualOrderDropDelegate: DropDelegate {
    let targetID: String
    @Binding var draggedID: String?
    let move: (_ draggedID: String, _ targetID: String) -> Void

    /// Spelled as properties rather than inline in the callbacks because both
    /// decide from `draggedID` alone and neither reads `DropInfo`, which has no
    /// public initializer for tests to build.
    ///
    /// Deliberately *not* wired to `validateDrop`: `onDrag` sets `draggedID` as
    /// a side effect, and if SwiftUI defers that write past the first
    /// `validateDrop` the gesture is refused for the whole session. `dropUpdated`
    /// is polled continuously, so gating there resolves within a frame instead.
    var acceptsDrop: Bool { draggedID != nil }

    /// The row to move, or nil when the pointer sits on the dragged row itself.
    ///
    /// Known hole: a drag released outside the list — or cancelled with Esc —
    /// never reaches `performDrop`, so `draggedID` keeps naming that row, and a
    /// later *text drag from another app* onto this list would be taken for it.
    /// Closing that needs a private drag type, which `itemProvider` explains why
    /// this file does not yet use.
    var pendingMoveSource: String? {
        guard let draggedID, draggedID != targetID else { return nil }
        return draggedID
    }

    func dropEntered(info: DropInfo) {
        guard let source = pendingMoveSource else { return }
        move(source, targetID)
    }

    func dropUpdated(info: DropInfo) -> DropProposal? {
        DropProposal(operation: acceptsDrop ? .move : .cancel)
    }

    /// `false` when no ATM drag is in flight, so a foreign drop falls through to
    /// whatever else wants it rather than being silently swallowed here.
    func performDrop(info: DropInfo) -> Bool {
        guard acceptsDrop else { return false }
        draggedID = nil
        return true
    }
}

extension View {
    /// One call per row so the drag type, the item provider and the drop delegate
    /// cannot drift apart between the lists that offer manual ordering.
    func atmManualOrderRow(
        id: String,
        dragged: Binding<String?>,
        move: @escaping (_ draggedID: String, _ targetID: String) -> Void
    ) -> some View {
        onDrag {
            dragged.wrappedValue = id
            return ATMManualOrder.itemProvider(for: id)
        }
        .onDrop(
            of: [.text],
            delegate: ATMManualOrderDropDelegate(targetID: id, draggedID: dragged, move: move)
        )
    }
}
