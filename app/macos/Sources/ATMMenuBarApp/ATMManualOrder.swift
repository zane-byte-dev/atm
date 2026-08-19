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
    private static let dragProviderNamePrefix = "atm-manual-order:"

    /// `public.utf8-plain-text` via the plain `NSItemProvider(object:)`
    /// initializer. Plainer than it looks, and deliberate.
    ///
    /// A private exported type does not survive SwiftUI's macOS drag bridge, so
    /// the provider remains plain text. `suggestedName` supplies a synchronous,
    /// process-owned marker that the drop delegate can verify before it moves a
    /// row; the text payload alone is not enough because a cancelled drag leaves
    /// the row binding alive until another drop ends the session.
    static func itemProvider(for id: String) -> NSItemProvider {
        let provider = NSItemProvider(object: id as NSString)
        provider.suggestedName = dragProviderNamePrefix + id
        return provider
    }

    static func owns(_ provider: NSItemProvider, for id: String) -> Bool {
        provider.suggestedName == dragProviderNamePrefix + id
            && provider.canLoadObject(ofClass: NSString.self)
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
    /// No `validateDrop`: `dropUpdated` and `dropEntered` already gate on this,
    /// and unlike them `validateDrop` is a one-shot per session — a false there
    /// costs the whole gesture, and it buys nothing the other two do not.
    var acceptsDrop: Bool { draggedID != nil }

    /// The row to move, or nil when the pointer sits on the dragged row itself.
    ///
    /// A cancelled drag can leave this source ID alive, so callbacks must also
    /// verify the provider marker before treating it as the current drag.
    var pendingMoveSource: String? {
        guard let draggedID, draggedID != targetID else { return nil }
        return draggedID
    }

    private func acceptsDrop(_ info: DropInfo, sourceID: String) -> Bool {
        info.itemProviders(for: [.text]).contains {
            ATMManualOrder.owns($0, for: sourceID)
        }
    }

    func dropEntered(info: DropInfo) {
        guard let source = pendingMoveSource, acceptsDrop(info, sourceID: source) else { return }
        move(source, targetID)
    }

    func dropUpdated(info: DropInfo) -> DropProposal? {
        guard let source = draggedID else { return DropProposal(operation: .cancel) }
        return DropProposal(operation: acceptsDrop(info, sourceID: source) ? .move : .cancel)
    }

    /// `false` when no ATM drag is in flight, so a foreign drop falls through to
    /// whatever else wants it rather than being silently swallowed here.
    func performDrop(info: DropInfo) -> Bool {
        guard let source = draggedID else { return false }
        let accepted = acceptsDrop(info, sourceID: source)
        draggedID = nil
        return accepted
    }
}

extension View {
    /// One call per row so the drag type, the item provider and the drop delegate
    /// cannot drift apart between the lists that offer manual ordering.
    func atmManualOrderRow(
        id: String,
        title: String,
        dragged: Binding<String?>,
        move: @escaping (_ draggedID: String, _ targetID: String) -> Void
    ) -> some View {
        // A compact name chip, not the row itself. Two rounds of trying to drag a
        // copy of the row taught the same lesson twice: the system draws the drag
        // image with its own alpha, which nothing reachable from `onDrag` can turn
        // off, so a row-sized preview always lets the rows it passes read through
        // it and looks like the list doubled. What is actually fixable is how much
        // area gets to collide — a chip barely overlaps anything, and reads as the
        // object being carried rather than a second copy of the list.
        //
        // Ground is still required: with no preview at all the drag image is a
        // fully transparent snapshot, which is where this started. `elevated` with
        // a border was tried too and read as a stray white slab, being pure white
        // in light appearance against a near-white `listPane`.
        let preview = Text(title)
            .font(ATMFont.font(.body, weight: .medium))
            .foregroundStyle(ATMTheme.primary)
            .lineLimit(1)
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(ATMTheme.listPane, in: RoundedRectangle(cornerRadius: ATMRadius.control))
        return onDrag({
            dragged.wrappedValue = id
            return ATMManualOrder.itemProvider(for: id)
        }, preview: { preview })
        .onDrop(
            of: [.text],
            delegate: ATMManualOrderDropDelegate(targetID: id, draggedID: dragged, move: move)
        )
    }
}
