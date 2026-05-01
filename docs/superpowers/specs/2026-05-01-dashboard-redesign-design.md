# agent-gate Dashboard Redesign

Date: 2026-05-01

## Goal

Redesign the local `agent-gate dashboard` from a plain HTML table interface into a cybersecurity-focused SOC console. The dashboard should feel like a serious local traffic audit tool: dark, dense, operational, and optimized for quickly spotting risky outbound requests.

The redesign must preserve the current Go template + HTMX/SSE stack. It should not introduce a frontend framework, a build step, or a database schema change.

## Approved Direction

Use the **SOC Console** direction:

- Dark console-style shell with restrained cybersecurity styling.
- Clear live-capture state in the header.
- Summary metrics for current dashboard scope.
- Strong severity hierarchy for high, medium, low, and info flags.
- Dense session/event tables that remain fast to scan.
- Prominent but controlled handling for dangerous actions such as raw event viewing and clearing captured events.

## Scope

In scope:

- Redesign the dashboard base layout, home page, session detail page, and event detail page.
- Add lightweight dashboard summary data in Go handlers using existing query results.
- Improve semantic structure and CSS class names where needed to support the new layout.
- Preserve live session-list refresh behavior with the existing HTMX/SSE mechanism.
- Preserve existing dashboard actions: clear events, trust host, dismiss flag, redacted/raw event views.
- Add or update tests for the new view model and existing behavior.

Out of scope:

- No new frontend framework.
- No TypeScript, bundler, npm dependency, or asset build pipeline.
- No database schema migration.
- No full-text body search.
- No new host detail pages or standalone flag-management pages.
- No change to proxy capture behavior or policy evaluation.

## Home Page Design

The home page becomes an analyst overview:

- A dark top bar with `agent-gate`, local dashboard context, and a visible live-capture indicator.
- A compact telemetry strip showing honest values computed from the current query result:
  - session or host group count
  - total captured events
  - flagged group count
  - severity counts, especially high and medium
  - latest captured event time
- A filter bar styled as console controls, keeping the current host/since/until query behavior.
- A high-density sessions table with strong visual hierarchy:
  - session/host label as the primary entry point
  - latest host
  - event count
  - flag/severity status
  - latest event timestamp
- A risk feed or review summary that surfaces the most important active conditions using current aggregate data.
- The destructive "Clear all events" action remains available but visually separated from normal review controls.

Empty states should stay useful and direct: no matching sessions for the current filters, with clear access to remove filters.

## Session Detail Design

Session detail becomes an investigation timeline:

- Breadcrumb back to sessions plus the current session or host-bucket label.
- Compact metadata/summary area for event count, flagged event count, and latest event time where available.
- Event table optimized for scanning:
  - time
  - method
  - host
  - path
  - status
  - flag chips
  - view action
- Flagged rows should stand out through border, chip, or row accent styling while still preserving dense table readability.
- Host-bucket sessions must keep the existing behavior of showing empty-session events grouped by normalized host.

## Event Detail Design

Event detail becomes a split inspection view:

- Breadcrumb back to sessions and session detail when a session id exists.
- Event summary band with method, URL, status, timestamp, capture mode, and flags.
- Request and response areas styled as dark code inspection panes.
- Headers remain collapsible to avoid pushing body content too far down the page.
- Redacted view remains the default.
- Raw view must be visibly dangerous:
  - strong warning state
  - explicit "secrets visible" wording
  - clear link back to redacted view
  - existing raw peek logging preserved

Long request/response bodies must remain readable with wrapping and scrolling behavior appropriate for JSON/text payloads.

## Backend/View Model

Keep data derivation inside the dashboard handler layer. Use existing store index rows and stored events; do not alter persistence.

For the sessions list, compute a small summary from the queried rows before rendering:

- total queried events
- total rendered groups
- number of groups containing any flags
- severity counts derived from known built-in flag-code severities
- unknown flag-code count when a code has no dashboard severity mapping
- latest event timestamp

If a flag code has no known severity mapping, the UI must not pretend it knows exact severity. It should count that condition as flagged and render a generic "flagged" or "unknown severity" state.

## Styling

Use a restrained SOC palette rather than a one-note neon theme:

- deep neutral backgrounds
- muted panel borders
- green/teal for live/clear/trusted states
- red/coral for high severity and raw-danger states
- amber for medium warning states
- cool gray for low/info/passive metadata

The UI should be compact but not cramped. Tables should use stable row heights, clear alignment, and predictable responsive behavior. Mobile/narrow widths should retain readable controls and tables, even if secondary metadata wraps.

## Testing

Update or add Go tests around dashboard rendering behavior:

- root page still renders a full HTML document and static assets
- session rows still render from store data
- empty-session events still group by normalized host
- dashboard summary values render from stored rows
- flagged state renders when stored rows have flags
- session detail still lists events
- event detail still redacts by default
- raw view still exposes bytes only when requested and logs `raw_peek`

Manual visual verification:

- run the dashboard locally
- inspect the home page, session detail, and event detail in the in-app browser
- check desktop and narrow viewport behavior
- confirm no console-breaking missing assets or unreadable text

## Acceptance Criteria

- The dashboard visually reads as a cybersecurity/SOC-style audit console.
- The redesigned home page surfaces meaningful summary data from real stored rows.
- Existing dashboard workflows still work: filter, session navigation, event detail, redacted/raw switch, clear events, trust host, dismiss flag.
- Existing HTMX/SSE refresh continues to update the session table.
- Tests cover the new summary rendering and existing safety-critical behavior.
