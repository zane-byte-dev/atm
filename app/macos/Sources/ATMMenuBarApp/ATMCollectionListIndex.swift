import Combine
import Foundation

/// Derived once per collection revision. A row reads its source, unread count
/// and supplements without rescanning the complete collection snapshot.
struct ATMCollectionListIndex {
    let orderedSources: [ATMCollectionSource]
    let sourcesByID: [String: ATMCollectionSource]
    let itemsByID: [String: ATMCollectionItem]
    let primaryItems: [ATMCollectionItem]
    let ignoredItems: [ATMCollectionItem]
    let primaryItemsBySource: [String: [ATMCollectionItem]]
    let unknownItems: [ATMCollectionItem]
    let groupedAllItems: [ATMCollectionItem]
    let flattenedItems: [ATMCollectionItem]
    let supplementsByItemID: [String: [ATMCollectionItem]]
    let unreadCountsByItemID: [String: Int]
    private let primaryIDs: Set<String>

    init(items: [ATMCollectionItem] = [], sources: [ATMCollectionSource] = [], sourceOrder: String = "") {
        let ordered = ATMManualOrder.ordered(sources, stored: sourceOrder, id: \.id)
        let sourceLookup = Dictionary(sources.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        orderedSources = ordered
        sourcesByID = sourceLookup

        // Archived and active records for the same Todo are separate families.
        // A new append must not disappear under an already archived create.
        struct Family: Hashable {
            let todoID: String
            let collapsed: Bool
            init?(_ item: ATMCollectionItem) {
                guard let id = item.todoID, !id.isEmpty else { return nil }
                todoID = id
                collapsed = item.shouldCollapseInCollection
            }
        }
        var creates = Set<Family>()
        var appends: [Family: [ATMCollectionItem]] = [:]
        for item in items {
            guard let family = Family(item) else { continue }
            if item.action == "create" { creates.insert(family) }
            if item.action == "append" { appends[family, default: []].append(item) }
        }
        for family in Array(appends.keys) {
            appends[family]?.sort {
                let lhs = $0.occurredAt ?? $0.createdAt
                let rhs = $1.occurredAt ?? $1.createdAt
                return lhs == rhs ? $0.id < $1.id : lhs < rhs
            }
        }
        let visible = items.filter { item in
            guard item.action == "append", let family = Family(item) else { return true }
            return !creates.contains(family)
        }
        itemsByID = Dictionary(visible.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let primary = visible.filter { !$0.shouldCollapseInCollection }
        let ignored = visible.filter(\.shouldCollapseInCollection)
        let bySource = Dictionary(grouping: primary, by: \.sourceID)
        let unknown = primary.filter { sourceLookup[$0.sourceID] == nil }
        primaryItems = primary
        ignoredItems = ignored
        primaryIDs = Set(primary.map(\.id))
        primaryItemsBySource = bySource
        unknownItems = unknown
        groupedAllItems = primary + ignored
        flattenedItems = ordered.flatMap { bySource[$0.id] ?? [] } + unknown + ignored

        var supplements: [String: [ATMCollectionItem]] = [:]
        var unreadCounts: [String: Int] = [:]
        for item in visible {
            let children: [ATMCollectionItem]
            if item.action == "create", let family = Family(item) {
                children = appends[family] ?? []
            } else {
                children = []
            }
            supplements[item.id] = children
            unreadCounts[item.id] = (item.isUnread ? 1 : 0) + children.filter(\.isUnread).count
        }
        supplementsByItemID = supplements
        unreadCountsByItemID = unreadCounts
    }

    func selectedItem(id: String?, flat: Bool, showingIgnored: Bool) -> ATMCollectionItem? {
        if let id, let item = itemsByID[id], flat || showingIgnored || primaryIDs.contains(id) {
            return item
        }
        if flat { return flattenedItems.first }
        return showingIgnored ? groupedAllItems.first : primaryItems.first
    }
}

/// The shared Store publishes many unrelated changes. Only collection content
/// and manual source order invalidate this index; a selection never does.
final class ATMCollectionListIndexStore: ObservableObject {
    @Published private(set) var index = ATMCollectionListIndex()
    private var items: [ATMCollectionItem] = []
    private var sources: [ATMCollectionSource] = []
    private var sourceOrder = ""

    func update(items: [ATMCollectionItem], sources: [ATMCollectionSource], sourceOrder: String) {
        guard self.items != items || self.sources != sources || self.sourceOrder != sourceOrder else { return }
        self.items = items
        self.sources = sources
        self.sourceOrder = sourceOrder
        index = ATMCollectionListIndex(items: items, sources: sources, sourceOrder: sourceOrder)
    }
}
