# Nexus design language

Nexus has two personalities. Underneath, it is a serious database tool.
On the surface, it should feel alive. This document is the contract for the
surface: colour, typography, the mascot and the voice. All of it lives in
`internal/ui`, `internal/ui/mascot` and `internal/tui`, and none of it is
allowed to influence behaviour — `--plain` and `--json` remove the surface
entirely and every command still works.

## Principles

1. **Hierarchy first.** Colour, weight and spacing exist to show what matters.
   Nothing is coloured just to be colourful.
2. **One idea per line.** Short headlines, details indented beneath them.
3. **Calm by default.** Animation appears only while something is actually
   happening, never loops forever, and never on non-interactive output.
4. **Honest.** The UI never shows a service, number or suggestion that isn't
   backed by real state.

## Layout rhythm

Every block of output is separated by exactly one blank line. A block leads
with a glyph and a headline; details are indented two cells beneath it.

```
✦ nexus is awake.

  Connected to PostgreSQL
  14 tables · 2.8 MB · 0 problems

  ready when you are.
```

Progress lives on stderr (so stdout stays pipeable) and settles into one
line per step, with timing only when it's meaningful (≥ 100 ms):

```
    ✓ PostgreSQL          16.4 · native · 127.0.0.1:54320 · fresh database  733ms
    ✓ migrations          1 applied · 1 table changed · 1 addition
```

## Glyphs

| glyph | meaning | plain |
|:-----:|---|:-:|
| ✦ | nexus speaking | `*` |
| ◈ | success, insight | `+` |
| ▲ | needs attention | `!` |
| ✕ | failed | `x` |
| ◇ | neutral information | `i` |
| → | next step | `->` |
| ✓ – ○ | step done / skipped / pending | `ok` `-` `o` |
| ● ○ | live / stopped | `*` `o` |
| ◆ ↗ | primary key / foreign key | `PK` `FK` |

Unicode glyphs are chosen to be single-cell in mainstream terminal fonts.

## Colour

| role | dark | light | used for |
|---|---|---|---|
| primary | `#A48BFF` | `#6A43F0` | nexus's voice, headings, selection |
| secondary | `#3CDCEB` | `#0A8FA8` | values, commands, numbers |
| spark | `#FFD6FF` | `#B03CC8` | the mascot's spark, rare highlights |
| strong / text | `#F7F7FB` / `#D9D9E3` | `#14141C` / `#2E2E3A` | emphasis / body |
| muted / faint | `#8A8AA0` / `#4A4A5E` | `#6B6B80` / `#B9B9C8` | labels, metadata / rules, borders |
| success · warning · error · info | `#5BE49B` `#FFC145` `#FF6B7A` `#6CB4FF` | darker variants | semantic states |
| database · auth · storage · realtime · functions · jobs · AI | domain accents | | small badges only, never body text |

Colours are hex; termenv degrades them to 256 or 16 colours automatically.
The light palette is selected with `NEXUS_THEME=light` or via `COLORFGBG`.
Nexus never queries the terminal for its background colour, because that can
stall over SSH and inside tmux.

The **core gradient** runs from electric violet `#7B5CFF` to signal cyan
`#1FC8E3`. It is the brand: the mascot's body and the `N E X U S` wordmark.

## Nex, the Nexus core

The mascot is **Nex**: a small, round, pastel data-being with big dark eyes,
a little `ω` mouth, rosy cheeks, stubby feet and a spark for an antenna. Its
little arms hold two network nodes — it is, literally, a nexus. The full
character guide is [MEET-NEX.md](MEET-NEX.md).

```
       ✦
    ▗▄▄▄▄▄▖
   ▟ ◕ ω ◕ ▙
●──▜▃     ▃▛──●
    ▝▀▘ ▝▀▘
```

### Construction

- 15 × 5 cells. Every frame of every mood has exactly this size so
  animations redraw in place without jitter (enforced by tests).
- In colour, the body is solid: background-coloured cells plus quadrant and
  three-quadrant blocks give a soft rounded shape with a lavender
  (`#A08CFF`) → aqua (`#6FE3F2`) gradient. Eyes and mouth are dark ink
  (`#1A1236`) on the body; cheeks are half-height pink (`#FF8FBF`) blush marks.
- Without colour, the same geometry becomes line art (`╭─╮ │ ┤├ ╰┬─╯`) with
  `·` cheeks. In plain mode Nex is not drawn at all.
- Three sizes: **portrait** (15×5), **face** (a 7-cell pill `▐ ◕ω◕ ▌` for
  headers and prompts), **glyph** (one cell).
- Speech bubbles (`Theme.Speak`) put words beside the portrait, joined by a
  short tail at face height.

### Moods

| mood | face | pose / extras | used when |
|---|---|---|---|
| idle | `◕ω◕` | blinks, glances left and right | everything is fine |
| waving | `◠ᴗ◠` | right arm waves | greetings, welcome |
| thinking | `◔~◔` | antenna twinkles, dots appear | waiting on analysis |
| working | `•ᴗ•` | packets flow inward | running something |
| connecting | `◕o◕` | nodes fill as packets reach them | starting up |
| success | `◠ᴗ◠` | green nodes | it worked |
| celebrating | `✧ᴗ✧` | arms raised, sparkles | big moments |
| love | `♥ᴗ♥` | floating hearts | `nexus pet` |
| curious | `◕o◉` | `?` antenna, glancing | nexus noticed something |
| warning | `◉~◉` | `!` antenna, amber nodes | needs attention |
| error | `×^×` | links broken | failed |
| sleeping | `‿.‿` | arms lowered, dimmed, drifting `z` | database offline |

### When Nex appears

Sparingly. The portrait appears for first-run and social moments (`init`,
the welcome screen, `hi`, `pet`, `mascot`) and the offline dashboard. The
face appears in headers (dashboard, SQL shell, status panels, doctor) where
it reflects real state. Everywhere else Nexus speaks with a single glyph.

## Motion

- The spinner is the spark itself: `· ✧ ✦ ✦ ✧ ·`, with a soft highlight
  sweeping across the label.
- Mascot animations are short sequences (the wake-up at `nexus init` is
  under two seconds) and only run on interactive terminals.
- `--no-animation`, `NEXUS_NO_ANIMATION`, `CI`, pipes and `--plain` all
  produce static output with identical content.

## Voice

Nexus speaks in lowercase, in short, calm, warm sentences. It is clever and
friendly, sometimes a little playful ("databases never really sleep."),
never obnoxious, never sarcastic, and never lets personality get between you
and the information. Exclamation marks are rare and reserved for genuinely
happy moments, like a pat. Errors always say exactly what broke and what to
do next — a joke is never a substitute.

| good | bad |
|---|---|
| `✦ nexus is awake.` | `✓ Success` |
| `◈ nice. 1 migration applied.` | `Operation completed successfully` |
| `✦ nexus noticed something.` | `WARNING!!!` |
| `◈ that's better.` | `OMG you fixed it 🎉🎉` |
| `✕ nexus is asleep.` → `wake it with nexus dev` | `Error: connection refused` |

The core phrase catalogue lives in `internal/ui/voice.go` (tests keep it
lowercase and calm); Nex's greetings, tips and reactions live in
`internal/cli/nex.go`.

### Errors

Every error answers three questions: what happened, why, what to do next.

```
✕ migration 20261002145504_add_posts failed.

  type "integr" does not exist
  migrations/20261002145504_add_posts.sql:11:14 (up section, rolled back)

   9 │   published  boolean not null default false,
  10 │   created_at timestamptz not null default now(),
  11 │   views      integr not null default 0
     │              ^

  → fix the file and run nexus migration apply again
```
