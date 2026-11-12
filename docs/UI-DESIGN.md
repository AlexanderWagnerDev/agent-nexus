# AgentNexus — UI design notes

This document records the visual system behind the 0.2 redesign and the reference
material used. It is a working design document, not a claim that every surface
has passed a formal accessibility audit.

## Reference analysis

The redesign uses two current developer tools as references, without copying
their branding, assets or source code:

- **OpenCode desktop** (`https://opencode.ai/desktop`, press assets at
  `https://opencode.ai/press`) — primary reference for the chat surface:
  near-black canvas, quiet chrome, a single rounded composer, plain assistant
  prose, inline code, compact tool rows and a strong focus on the conversation.
- **Paseo** (`https://paseo.sh`, `https://github.com/getpaseo/paseo`) — primary
  reference for orchestration: a project/session rail, session tabs, a review
  panel beside the conversation, explicit agent status, and a mobile client that
  keeps the session list and composer reachable.

The resulting direction is roughly 60% OpenCode (chat and typography) and 40%
Paseo (projects, sessions, status and panels), expressed through AgentNexus'
own neutral blue/indigo identity.

## Design principles

1. The conversation is the product. Chrome stays quiet and secondary.
2. Information density without tiny type: 14–15 px UI text, 15–16 px chat text,
   13 px code and 12–13 px metadata.
3. Few containers. Borders and spacing carry hierarchy before boxes and shadows.
4. One accent (`#7d8cf8` dark, `#4056d6` light). Semantic green/amber/red are
   reserved for state.
5. No gradients, glass effects, decorative illustration or oversized cards.
6. Technical detail is available on demand: tools collapse, raw input is nested,
   reasoning is collapsed, and Git diffs live in a separate panel.
7. Light and dark are designed together; neither is a filter over the other.

## Tokens

| Token | Dark | Light |
| --- | --- | --- |
| Canvas | `#0f1012` | `#f7f7f8` |
| Surface | `#161719` | `#ffffff` |
| Raised surface | `#1c1e21` | `#f1f2f4` |
| Border | `#2b2d32` | `#e1e3e7` |
| Text | `#e8e9eb` | `#1c1d20` |
| Muted | `#9a9da4` | `#63666d` |
| Accent | `#7d8cf8` | `#4056d6` |
| Radius | 2-4 px | 2-4 px |
| UI type | 14 px / 1.5 | same |
| Chat type | 15 px / 1.65 | same |
| Code type | 13 px mono | same |

The sidebar is 280 px expanded and 64 px collapsed. The top bar is 58 px on
desktop and 54/50 px on mobile. The right context panel defaults to 320 px and
can be dragged between 240 and 520 px; its width is stored locally.

## Layout

- **Left rail**: Home, Projects, Sessions, Active, Hosts, Settings, followed by
  pinned sessions, project groups (expandable to recent sessions), recent
  sessions and hosts.
- **Main**: page header with actions, a global tab strip, then the page.
- **Chat**: status bar, conversation column (max 900 px), composer aligned to
  the same column, optional right context panel.
- **Context panel**: run/session identity, native id and path with copy buttons,
  lazy Git status/diff, favorite/transcript/terminal/export actions, continuity
  note.
- **Mobile**: bottom navigation (Home, Projects, Sessions, Active, Hosts,
  Settings), hidden tabs, a chat-height composer that stays above the navigation,
  and a full-width context drawer.

## Chat model

- User turns are right-aligned bubbles; assistant turns are plain prose with an
  agent marker, not cards.
- Markdown/GFM is rendered through a dependency-free, allowlist-based renderer:
  headings, lists, tables, blockquotes, links (HTTP(S)/mailto only), inline code
  and fenced code. Raw HTML is escaped, never interpreted.
- Code blocks have a language header, restrained token highlighting, scrolling
  and a copy button.
- Tool calls use compact rows: icon, human label, command/path/query, status, and
  an expandable body with command, output, diff and collapsed raw input. Shell,
  read, edit, search, web, Git, tests, plans, delegated tasks and MCP calls have
  distinct labels.
- Diffs render as a structured review panel: unified or side-by-side, old/new line numbers, per-file headers with change totals, file chips, syntax highlighting and a persisted layout choice.
- Reasoning is a collapsed “Thinking” row.
- Streaming is throttled to keep long sessions responsive; auto-scroll pauses
  when the reader scrolls up and offers “Jump to latest”.
- Native history loads the latest 200 messages and offers earlier pages without
  moving the reader’s viewport.

## Composer

- Auto-growing textarea (48–220 px), 15 px text, 12 px radius.
- Footer shows the agent, mode and host in small pills.
- Enter sends on desktop; Shift+Enter inserts a newline. On coarse pointers Enter
  inserts a newline and the send button is used, avoiding accidental sends.
- A separate interrupt button appears only while the agent is working.
- Drafts are stored per run in `localStorage` and restored on return.
- The send control is a compact icon button rather than a large call to action.

## Split workspace

- Two live sessions share one workspace, side by side or stacked. Each pane is
  an embedded same-origin chat iframe with its own stream and approvals.
- A draggable divider resizes the panes between 25% and 75%; orientation and
  ratio are persisted locally.
- Panes expose the session title, project, host, agent and live state, plus
  open-in-single-view, swap and close actions.
- On phones the panes stack and the divider is hidden. The iframe is absolutely
  positioned inside its pane so it always fills the available height instead of
  falling back to its intrinsic height.

## Responsive behavior

| Width | Behavior |
| --- | --- |
| > 1280 px | 280 px rail, full chat + context panel |
| 1100–1280 px | narrower rail, context panel 290 px |
| 900–1100 px | context panel becomes an overlay drawer |
| < 900 px | bottom navigation, single column, tabs hidden |
| < 640 px | icon-only chat actions, no top-bar theme shortcut (Settings still has it) |

All tested mobile layouts avoid horizontal page overflow; long strings, code and
tool output scroll inside their own containers.

## Latest refinement (alpha)

- Rectangular controls now use 2-4 px radii instead of 7-14 px. Circles remain
  reserved for status dots and spinners.
- **Review changes** opens a full-width `#/sessions/<id>/changes` page instead of
  a modal. Unified and split diffs use the whole content width, and the diff
  body has no nested height cap on that page. Split mode clips each side
  independently and falls back to unified below 520 px.
- German is available as a second interface language. English remains the
  default. The language can be changed on the login screen (EN/DE) or under
  **Settings -> Appearance -> Language**, is stored per browser, and updates
  `<html lang>`. Agent names, mode identifiers, paths and transcript content are
  never translated.

## Virtualized session list

The sessions page renders a window of roughly 20-35 rows inside a fixed-height
scroll container. Group headers and rows have fixed heights (38 px and 56 px),
so the scrollbar is backed by a spacer while only the visible window plus
600 px of overscan is in the DOM. Server-side pages of 200 sessions load
automatically through an `IntersectionObserver`; a generation check prevents a
slow page response from overwriting a newer route. The 10,000-session test lives
in `web/e2e/virtual.mjs`.

## Visual regression

`cd web && npm run e2e:visual` compares four stable views against
`web/e2e/demo/visual-baseline/` with a pinned clock (`DEMO_NOW`) and the
synthetic demo. Differences are written to `visual-report/`. After an intended
UI change, regenerate the baselines with `npm run e2e:visual:update` and review
the PNG diff. The scheduled/manual GitHub workflow runs the same comparison on
a Windows runner; it is not a pull-request gate because font rasterisation
differs between the reference machine and CI.

## Known gaps

- ACP sessions expose model, reasoning, mode and image-attachment controls only
  when the agent advertises the corresponding session config options, modes or
  image prompt capability. This is tested with an in-process fake ACP agent,
  not against a real `claude-agent-acp`.
- Agent catalogs can be large; the model selector groups by provider and offers
  a lightweight fuzzy filter without an extra dependency.
- The Markdown renderer supports GFM tables, footnotes and nested task lists.
  Raw HTML remains escaped and is never interpreted.
- No formal WCAG 2.2 AA audit has been performed. Semantic controls, labels,
  focus styles, reduced-motion handling and contrast were reviewed manually.
- The renderer and tool formatting have unit tests; chat behavior is verified by
  the browser smoke test and manual sessions. A pixel-diff suite covers four
  stable desktop views (login, sessions, hosts, settings); the activity views
  are excluded because they render live elapsed times for the scripted demo
  runs.
  Baselines currently come from Windows 11; the manual/nightly CI job uses a
  wider tolerance on the Windows runner, and no Linux/macOS reference baselines
  exist yet.