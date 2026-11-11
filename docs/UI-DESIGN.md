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
| Radius | 7–14 px | 7–14 px |
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
- Diff lines use added/removed/hunk styling in both Git and tool output.
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

## Known gaps

- Split view with two simultaneous chat panes is not implemented; session tabs
  and the control room cover switching and monitoring.
- File attachments, model/thinking selection and agent-mode selection inside the
  composer are not exposed because the host protocols currently used do not
  provide a stable contract for them.
- The Markdown renderer is intentionally small; footnotes, nested task lists and
  complex tables may fall back to plain text.
- No formal WCAG 2.2 AA audit has been performed. Semantic controls, labels,
  focus styles, reduced-motion handling and contrast were reviewed manually.
- The renderer and tool formatting have unit tests; chat behavior is verified by
  the browser smoke test and manual sessions, not by a full visual regression
  suite.
