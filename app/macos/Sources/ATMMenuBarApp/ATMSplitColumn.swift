import AppKit
import SwiftUI

/// Width bookkeeping for the workspace two-column layout, kept free of SwiftUI so
/// the clamping rules can be tested without a window.
enum ATMSplitColumnWidth {
    /// The visible hairline between the columns.
    static let dividerWidth: CGFloat = 1
    /// The invisible drag target around it. A 1pt grab zone is not hittable, and
    /// widening the *visible* line to match would put a grey band between every
    /// two columns.
    static let handleWidth: CGFloat = 10

    static func defaultsKey(_ id: String) -> String { "ATMSplitColumnWidth.\(id)" }

    /// Resolves a requested sidebar width against its own range and against what
    /// the window can actually give it.
    ///
    /// The detail pane keeps `detailMinWidth`; past that the sidebar gives ground,
    /// because the detail pane is the one holding the document. If not even the
    /// sidebar minimum fits, the minimum wins and the detail pane takes the
    /// remainder — a sidebar squeezed below its minimum is unusable, while a
    /// narrow detail pane still scrolls.
    static func resolve(
        requested: CGFloat,
        available: CGFloat,
        range: ClosedRange<CGFloat>,
        detailMinWidth: CGFloat
    ) -> CGFloat {
        // A NaN compares false against everything, so it would slip through the
        // clamp below and land in a frame width, blanking the pane. Infinities
        // need no special case — they clamp to the range like any other number.
        let requested = requested.isNaN ? range.lowerBound : requested
        // Whole points only. A fractional width makes the sidebar's own layout
        // round differently from the frame it was given, which showed up as the
        // column twitching by a point while being dragged.
        let width = min(max(requested.rounded(), range.lowerBound), range.upperBound)
        // Floored, not rounded: rounding up here would take half a point back off
        // the detail pane's minimum.
        let ceiling = (available - detailMinWidth - dividerWidth).rounded(.down)
        guard available.isFinite, ceiling >= range.lowerBound else { return range.lowerBound }
        return min(width, ceiling)
    }
}

/// Sidebar + detail with a draggable divider whose width survives relayout and
/// app restarts.
///
/// This replaces `HSplitView`, which forgets its divider position: SwiftUI hands
/// AppKit fresh ideal widths whenever the surrounding hierarchy changes, so the
/// column snapped back to its default after something as ordinary as creating a
/// task (the add-task overlay animates the whole window content). The width lives
/// in `UserDefaults` under `id`, which also makes it persist across launches.
struct ATMSplitColumn<Sidebar: View, Detail: View>: View {
    private let range: ClosedRange<CGFloat>
    private let detailMinWidth: CGFloat
    private let sidebar: Sidebar
    private let detail: Detail

    @AppStorage private var storedWidth: Double
    /// Live width while dragging. Committing to `@AppStorage` only on release
    /// keeps the drag smooth and off `UserDefaults`.
    @State private var draggedWidth: CGFloat?
    @State private var dragOrigin: CGFloat?
    @State private var isHoveringDivider = false
    @State private var cursorPushed = false

    init(
        id: String,
        defaultWidth: CGFloat,
        minWidth: CGFloat,
        maxWidth: CGFloat,
        detailMinWidth: CGFloat,
        @ViewBuilder sidebar: () -> Sidebar,
        @ViewBuilder detail: () -> Detail
    ) {
        range = minWidth...max(minWidth, maxWidth)
        self.detailMinWidth = detailMinWidth
        self.sidebar = sidebar()
        self.detail = detail()
        _storedWidth = AppStorage(
            wrappedValue: Double(defaultWidth),
            ATMSplitColumnWidth.defaultsKey(id)
        )
    }

    var body: some View {
        GeometryReader { proxy in
            let available = proxy.size.width
            let width = resolved(available: available)
            HStack(spacing: 0) {
                sidebar
                    .frame(width: width)
                divider(available: available, current: width)
                    // The grab area overhangs the hairline into both panes, and
                    // SwiftUI hit-tests later siblings first — without this the
                    // detail pane would swallow the right half of it.
                    .zIndex(1)
                detail
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            .frame(width: available, height: proxy.size.height)
        }
    }

    private func resolved(available: CGFloat) -> CGFloat {
        ATMSplitColumnWidth.resolve(
            requested: draggedWidth ?? CGFloat(storedWidth),
            available: available,
            range: range,
            detailMinWidth: detailMinWidth
        )
    }

    private func divider(available: CGFloat, current: CGFloat) -> some View {
        Rectangle()
            .fill(ATMTheme.border)
            .frame(width: ATMSplitColumnWidth.dividerWidth)
            .overlay {
                Color.clear
                    .frame(width: ATMSplitColumnWidth.handleWidth)
                    .contentShape(Rectangle())
                    .onHover { hovering in
                        isHoveringDivider = hovering
                        // Not popped mid-drag: a fast drag outruns the layout and
                        // reports a hover exit, and reverting to the arrow while the
                        // divider is still being dragged reads as a dropped grab.
                        setCursorPushed(hovering || isDragging)
                    }
                    .onDisappear { setCursorPushed(false) }
                    .gesture(
                        // Global coordinates on purpose. In the handle's own space
                        // the drag fought itself: the handle moves with the column,
                        // so every point the divider gained shifted the space under
                        // the cursor back by the same amount, shrinking the reported
                        // translation and making the column judder instead of follow.
                        DragGesture(minimumDistance: 1, coordinateSpace: .global)
                            .onChanged { value in
                                // Translation is measured from where the drag
                                // started, so the baseline has to be the width at
                                // that moment — reusing the live width would add
                                // the same offset again on every frame.
                                let origin = dragOrigin ?? current
                                dragOrigin = origin
                                draggedWidth = ATMSplitColumnWidth.resolve(
                                    requested: origin + value.translation.width,
                                    available: available,
                                    range: range,
                                    detailMinWidth: detailMinWidth
                                )
                            }
                            .onEnded { _ in
                                if let draggedWidth { storedWidth = Double(draggedWidth) }
                                draggedWidth = nil
                                dragOrigin = nil
                                setCursorPushed(isHoveringDivider)
                            }
                    )
            }
    }

    private var isDragging: Bool { dragOrigin != nil }

    /// Balanced push/pop. An unpaired push leaves the resize cursor stuck over the
    /// whole window; an unpaired pop takes someone else's cursor off the stack.
    private func setCursorPushed(_ pushed: Bool) {
        guard pushed != cursorPushed else { return }
        cursorPushed = pushed
        if pushed {
            NSCursor.resizeLeftRight.push()
        } else {
            NSCursor.pop()
        }
    }
}
