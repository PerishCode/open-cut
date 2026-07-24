# Creator visual language

Status: Product design baseline.

## Neutral Broadcast Matte

Open Cut is a desktop creative instrument, not a web dashboard. Its workspace
uses one stable dark neutral environment independent of the operating system's
light or dark preference. A stable matte reduces glare, keeps the program image
as the brightest creative surface, and avoids changing perceived media color
when the system theme changes.

The root surface roles, from lowest to highest, are:

1. canvas — gaps and the frame behind the workspace;
2. chrome — application and pane headers;
3. pane — persistent Sources, Timeline, and Agent regions;
4. raised — cards, selected tools, and bounded results;
5. input — editable or interactive wells;
6. media — the black Viewer and Timeline picture/track bed.

Hairlines, not large shadows or floating rounded containers, describe the
persistent panel topology. Shadows are reserved for genuinely raised temporary
or strong result surfaces. Media remains a stricter black subtree while using
the same text, selection, and focus hierarchy as the surrounding matte.

## Color semantics

The olive accent identifies product identity, creative selection, primary
action, and keyboard focus. It does not mean success.

Ready, pending, unavailable, and danger each have separate semantic colors.
Status must include text or another non-color cue. Disabled controls reduce
prominence but remain legible. Forced-colors and other operating-system
accessibility modes are not suppressed.

## Type and density

UI copy uses the shared sans family. Monospace is limited to timecode, durable
identifiers, compact technical facts, and uppercase eyebrow labels. Headings,
body copy, labels, and controls must use the closed shared type roles rather
than consumer-defined sizes.

The default shell uses a 54px application header, 36px pane headers, and 28px
standard controls. Compact media and placement strips may use 22px controls.
Space communicates grouping, not decoration: a panel must not grow large empty
padding merely to appear important.

## Action hierarchy

Each local decision surface has at most one primary action. Secondary actions
use the bounded secondary style; navigation, refresh, and reversible disclosure
use quiet actions. Destructive actions use the danger role and never borrow the
brand accent.

Selection, hover, focus, pending work, and committed outcome are distinct
states. Agent prose cannot receive committed styling without a durable product
receipt.

## Ownership and acceptance

`packages/components` owns tokens and visual atoms. Product consumers compose
those atoms and do not add private CSS, free-form color values, or styling props.
New recurring visual behavior becomes a shared closed variant only after at
least two real consumers need it.

Visual changes are accepted in the real Electron renderer. Normal phase work
uses `oc-control dev inspect --snapshot` for screenshots, browser-native
semantics, focus, geometry, overflow, and clipping. Exact generic role/name
clicks use the same inspect connection and must fail closed on ambiguous or
disabled targets. Playwright is reserved for isolated or high-concurrency
interaction suites. Any exploratory Playwright CLI session must have an
explicit task-recorded name, use `detach` for an attached Electron target or
`close` for a spawned browser, and be reconciled through `playwright-cli list
--json` before the phase ends.

Every tenth phase replays:

- product minimum native outer 1280×800;
- default native outer 1440×900;
- native fullscreen when the work or release gate needs native window evidence
  and the desktop session is unlocked.

At each size the document body must not become a scroll owner, persistent panes
must retain their own bounded overflow, focusable controls must have accessible
names, and the console must have no product errors.
