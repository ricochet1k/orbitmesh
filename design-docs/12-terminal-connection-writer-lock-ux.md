# Terminal Connection and Writer-Lock UX

## Status
- Alternatives
- Author: Claude (build agent)
- Date: 2026-02-14
- Task: Tw14d68
- Parent Review: Tiq2p2l (Usability review for custom terminal renderer)

## Why this design is needed

The usability review (Tiq2p2l) identified two critical UX gaps that are blockers for release:

1. **Connection state visibility**: Users must see when terminal output is stale (reconnecting, resyncing, snapshot drift) and when input is disabled to avoid acting on a frozen screen.
2. **Writer-lock management**: Users need explicit controls to take/release writer control, see who owns the lock, understand when lock is denied, and know when input is disabled.

Without these, users can:
- Type into a non-writable terminal with no feedback
- Act on stale/frozen terminal output thinking it's live
- Overwrite another user's active session without warning
- Be confused when commands don't work or disappear

## Project principles alignment

From project docs:
- **Operational reliability** (principle 2): Users need clear feedback about system state
- **Human-in-the-loop transparency** (principle 5): Make automation boundaries and control states visible
- **Low-friction workflows** (principle 7): Writer control should be discoverable and quick to acquire

## Alternative 1: Minimalist status line with modal dialogs

### Description
Add a compact status bar at the top of the terminal chrome showing connection state and lock owner. Use modal dialogs for lock acquisition/denial.

**Connection states shown:**
- 🟢 Connected (green dot)
- 🟡 Reconnecting... (amber dot + text)
- 🔄 Resyncing... (blue icon + text)
- ⚠️ Degraded (warning icon)

**Writer lock shown:**
- 🔒 Watching (gray lock icon)
- ✏️ You're writing (green pencil icon)
- 👤 alice@example.com is writing (user icon + email)

**Interactions:**
- Click lock icon → modal dialog to request writer control
- "Release control" button when you own the lock
- Input disabled with light overlay when not writer

### Assumptions
- WebSocket provides real-time connection state updates
- Backend tracks single writer per session with idle timeout (e.g. 5min)
- Lock acquisition is fast (<500ms)

### Pros
- Minimal screen real estate (single status line)
- Clear visual indicators (color-coded connection states)
- Modal prevents accidental lock requests
- Works well on mobile (touch-friendly buttons)

### Cons
- Modal dialog interrupts workflow when requesting lock
- Status bar may be missed by users focused on terminal output
- No persistent reminder that you're locked out (just disabled input)
- Color-only indicators fail accessibility standards

### Risks
- Users might not notice status bar and be confused why they can't type
- Modal delay could frustrate rapid workflows
- Idle timeout might kick users out mid-command

### Effort estimate
- Frontend: 2-3 days (status bar component, modal, WebSocket state handling)
- Backend: 1 day (lock state in session, WebSocket messages for lock events)
- Testing: 1 day (connection state transitions, lock denial scenarios)

---

## Alternative 2: Persistent command palette with inline lock controls

### Description
Add a small command palette/control strip below the terminal header that shows both connection status and lock controls inline.

**Connection status (left side):**
- Connected • Last update 2s ago
- Reconnecting (3s)...
- Resyncing from snapshot...
- Degraded - some events may be delayed

**Lock controls (right side):**
- [Take Control] button (when available)
- "You have control" + [Release] button (when writing)
- "alice@example.com has control" (when locked)
- Input disabled visual: grayed-out terminal + lock icon overlay

**Auto-expanding details:**
- Hover/click status for connection details (packets, latency)
- Hover lock for policy details (idle timeout, queue status)

### Assumptions
- Users prefer inline controls over modals
- Command palette space is acceptable overhead
- Connection metrics are available from WebSocket

### Pros
- Always visible, no hidden states
- No modal interruptions
- Control acquisition is one-click
- Shows last-update timestamp for stale output detection
- Can show rich connection details on demand

### Cons
- Takes more vertical space (50-60px)
- More complex UI with multiple elements
- May look cluttered on small screens
- Requires more comprehensive WebSocket event handling

### Risks
- Users might ignore the palette if it's always visible (banner blindness)
- Inline controls could be accidentally clicked
- May need responsive design for mobile

### Effort estimate
- Frontend: 3-4 days (command palette component, inline controls, hover states, responsive design)
- Backend: 1 day (same lock state + metrics exposure)
- Testing: 2 days (all interaction patterns, responsive layouts)

---

## Alternative 3: Terminal chrome integrated status with toast notifications

### Description
Integrate connection and lock status directly into the terminal header/chrome, use toast notifications for state transitions and lock denials.

**Terminal header integration:**
- Connection indicator badge (•) with color + tooltip
- Writer badge: "👤 Watching" or "✏️ Writing" with user email on hover
- Input layer: disabled state shows semi-transparent lock icon overlay on terminal

**Interactions:**
- Right-click or keyboard shortcut (Ctrl+Shift+W) to request lock
- Toast notifications:
  - "Writer control acquired"
  - "alice@example.com took writer control"
  - "Connection lost, reconnecting..."
  - "Lock denied: session is busy"
- Auto-dismiss toasts after 5s (or click to dismiss)

**Visual lock feedback:**
- Terminal border changes color (gray = watching, blue = writing)
- Input cursor only shows when you have lock
- Lock icon overlay when input is disabled

### Assumptions
- Users understand toast notification patterns
- Right-click/shortcut is discoverable enough
- Border color change is noticeable

### Pros
- Cleanest integration with existing terminal UI
- Non-blocking notifications for state changes
- Multiple feedback channels (border, badge, toast, overlay)
- Keyboard shortcut for power users
- Minimal permanent UI footprint

### Cons
- Lock acquisition less discoverable (no button)
- Toast notifications can be missed
- Right-click may conflict with terminal context menu
- Multiple visual indicators might be inconsistent

### Risks
- Users may not discover the right-click/shortcut
- Toast-only notification for connection issues might be too subtle
- Border color may not be obvious enough for lock state

### Effort estimate
- Frontend: 2-3 days (header badges, toast system, overlay, keyboard shortcuts)
- Backend: 1 day (same as alternatives 1-2)
- Testing: 1-2 days (toast timing, keyboard shortcuts, visual feedback)

---

## Alternative 4: Split-pane with dedicated connection/control panel

### Description
Add a collapsible side panel (or bottom panel) that shows detailed connection metrics and lock controls. Terminal takes majority of space, panel can be toggled.

**Panel contents:**
- Connection status timeline (last 10 events)
- Real-time metrics (latency, packets, queue depth)
- Writer control section with explicit "Request Control" / "Release Control" buttons
- Active writers list (for future multi-writer support)
- Lock policy settings (idle timeout, etc.)

**Terminal visual:**
- Simple badge in terminal header links to panel
- Input disabled overlay when not writer

### Assumptions
- Power users want detailed connection visibility
- Panel space is acceptable tradeoff
- Users will toggle panel as needed

### Pros
- Maximum information density
- Room for future features (multi-writer, metrics graphs)
- Doesn't clutter main terminal view when collapsed
- Clear control section for lock management

### Cons
- Most complex UI approach
- Takes significant screen real estate when open
- Overkill for simple use cases
- More implementation work

### Risks
- Users may never open the panel (hidden by default)
- Complex UI harder to maintain
- May feel overengineered for MVP

### Effort estimate
- Frontend: 5-6 days (panel component, metrics display, toggle states, responsive)
- Backend: 1-2 days (metrics exposure, control API)
- Testing: 2-3 days (panel states, metrics accuracy, control flows)

---

## Comparison matrix

| Criteria | Alt 1: Modal | Alt 2: Palette | Alt 3: Chrome+Toast | Alt 4: Panel |
|----------|--------------|----------------|---------------------|--------------|
| Discoverability | Medium | High | Low-Medium | Low |
| Screen efficiency | High | Medium | High | Low |
| Information density | Low | Medium | Low | High |
| Non-blocking | Low (modal) | High | High | High |
| Implementation effort | Low (6-7d) | Medium (8-10d) | Low-Medium (6-8d) | High (12-15d) |
| Mobile-friendly | High | Medium | High | Low |
| Accessibility | Medium | High | Medium | High |
| Future extensibility | Low | Medium | Medium | High |

---

## Recommendations for owner decision

This design must balance **discoverability, minimal disruption, and MVP timeline**. All alternatives meet the blocker requirements but differ in tradeoffs:

**For MVP / fastest path:** Alternative 1 (Minimalist) or Alternative 3 (Chrome+Toast)
- Both are ~6-8 days implementation
- Alternative 1 is more discoverable (button always visible)
- Alternative 3 is cleaner but requires keyboard shortcut discovery

**For best long-term UX:** Alternative 2 (Palette)
- Most balanced approach (visible, non-blocking, room to grow)
- ~8-10 days but sets good foundation

**Not recommended for MVP:** Alternative 4 (Panel)
- Too complex for initial release
- Could be added later if metrics/multi-writer needed

**Key decision points:**
1. Is modal interruption acceptable for lock requests? (affects Alt 1)
2. Is 50-60px vertical space acceptable? (affects Alt 2)
3. Can we rely on keyboard shortcuts for lock control? (affects Alt 3)
4. Do we need detailed connection metrics visible? (affects Alt 4)

---

## Open questions for review

1. Should connection state transitions trigger browser notifications (outside of window)?
2. What's the idle timeout for writer lock? (5min? configurable?)
3. Should we show lock queue (who's waiting for control)?
4. How do we handle forced lock release (admin action)?
5. Do we need "view-only mode" indicator for sessions with no write permission?

---

## Next steps

1. Owner reviews alternatives and selects one (or hybrid approach)
2. Designer expands chosen alternative into detailed design document with:
   - Precise component hierarchy and React component specs
   - WebSocket message protocol for connection/lock state
   - Visual design mockups (color scheme, icons, layouts)
   - Accessibility requirements (ARIA labels, keyboard navigation)
   - Error states and edge cases (connection flapping, lock races)
3. Architect reviews design and creates implementation tasks
4. Handoff to developer role

---

## References

- Parent review: Tiq2p2l (Usability review)
- Related design: design-docs/11-termemu-pty-websocket-activity-feed.md
- Project principles: (assumed from context)
