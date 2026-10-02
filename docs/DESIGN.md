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

## The Nexus core

The mascot is a small luminous data-being: a bevelled core with two eyes, a
spark above it, and network links reaching out to two nodes. It is,
literally, a nexus — the thing in the middle that everything connects to.

```
       ✦
    ▗▄▄▄▄▄▖
   ▟ ◉   ◉ ▙
●──▜       ▛──●
    ▝▀▀▀▀▀▘
```

### Construction

- 15 × 5 cells. Every frame of every state has exactly this size so
  animations redraw in place without jitter (enforced by tests).
- In colour, the body is solid: background-coloured cells plus quadrant and
  three-quadrant blocks (`▗▄▖ ▟▙ ▜▛ ▝▀▘`) give a rounded, glowing shape
  with a horizontal violet→cyan gradient. Eyes are drawn on the body.
- Without colour, the same geometry becomes line art (`╭─╮ │ ┤├`).
- In plain mode the mascot is not drawn at all.
- Three sizes: **portrait** (15×5), **face** (a 7-cell pill `▐◉ ◉▌` for
  headers and prompts), **glyph** (one cell).

### Expressions

| state | eyes | pose | used when |
|---|---|---|---|
| idle | `◉ ◉`, occasional blink | links level | everything is fine |
| thinking | `◔ ◔`, spark twinkles, dots appear | | waiting on analysis |
| working | `◉ ◉`, packets flow inward | | running something |
| connecting | nodes fill as packets reach them | | starting up |
| success | `◠ ◠ ◡` | | it worked |
| celebrating | `◠ ◠ ◡`, sparkles | links raised | big moments |
| curious | `◉ ◕`, `?` spark | | nexus noticed something |
| warning | `◉ ◉ ─`, `!` spark | amber nodes | needs attention |
| error | `× × ◠` | links broken | failed |
| sleeping | `─ ─`, drifting `z` | links lowered, dimmed | database offline |

See them all with `nexus mascot`, or one with `nexus mascot sleeping`.

### When the mascot appears

Sparingly. The portrait appears for first-run moments (`nexus init`), the
offline dashboard, and `nexus mascot`. The face appears in headers (dashboard,
SQL shell, status panels) where it reflects real state: sleeping when the
database is down, curious when there are pending migrations or health
findings. Everywhere else, Nexus speaks with a single glyph.

## Motion

- The spinner is the spark itself: `· ✧ ✦ ✦ ✧ ·`, with a soft highlight
  sweeping across the label.
- Mascot animations are short sequences (the wake-up at `nexus init` is
  under two seconds) and only run on interactive terminals.
- `--no-animation`, `NEXUS_NO_ANIMATION`, `CI`, pipes and `--plain` all
  produce static output with identical content.

## Voice

Nexus speaks in lowercase, in short, calm, confident sentences. It is
clever and occasionally warm, never obnoxious, never sarcastic, and never
more than one line of personality per command.

| good | bad |
|---|---|
| `✦ nexus is awake.` | `✓ Success` |
| `◈ nice. 1 migration applied.` | `Operation completed successfully` |
| `✦ nexus noticed something.` | `WARNING!!!` |
| `◈ that's better.` | `OMG you fixed it 🎉🎉` |
| `✕ nexus is asleep.` → `wake it with nexus dev` | `Error: connection refused` |

The phrase catalogue lives in `internal/ui/voice.go`. Tests enforce that
every phrase is lowercase and has no exclamation marks.

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
