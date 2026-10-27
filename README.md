# cade

```
 ██████╗ █████╗ ██████╗ ███████╗
██╔════╝██╔══██╗██╔══██╗██╔════╝
██║     ███████║██║  ██║█████╗
██║     ██╔══██║██║  ██║██╔══╝
╚██████╗██║  ██║██████╔╝███████╗
 ╚═════╝╚═╝  ╚═╝╚═════╝ ╚══════╝
```

A small terminal ADE for [Claude Code](https://claude.com/claude-code). The left sidebar lists your
projects and their Claude sessions as a tree; the right pane runs the real `claude` CLI for the
selected session. cade is only a wrapper: every key, paste and mouse event in the Claude pane goes
to Claude unchanged, so Claude's own shortcuts and commands keep working.

```
 SESSIONS               cade v0.1.0 │ cade › fix login flow (34b6e4a0)
 ▾ cade  ~/code/cade                │
   ├─ [n] new session               │   (claude runs here)
   ├─ ● fix login flow   (34b6e4a0) │
   └─ Initialise proje…  (00d20d9a) │
 ▸ other  ~/code/other              │
 ─────────────────────────────────  │
 [j/k] move   [⇧↑/⇧↓] project       │
 ...                                │
```

## Requirements

- Linux (macOS should work but is untested), and `claude` on your `PATH`.
- A terminal with the kitty keyboard protocol (kitty, foot, WezTerm, Ghostty, Alacritty) to get
  every key combination through, including `Ctrl+Esc`. Other terminals work with reduced key
  fidelity.
- 24-bit colour for the Dracula theme to look right.
- Docker, to build. The binary itself runs on the host.

## Build and run

```sh
make setup      # once: build the dev image
make build      # static binary at bin/cade
./bin/cade      # run from any directory
```

Run cade on the host, not inside the container: it needs your `claude` binary and `~/.claude`.

## Usage

On first start cade adds the directory you launched it from as a project. Add more with `a`.
Sessions are read from `~/.claude/projects` and refreshed every 2 seconds. A session's title is the
one set in cade with `r`, else its `/rename` name, else its agent name, else Claude's AI title,
else its first prompt. Starred sessions (`s`, `*` or a click on the ★ column) stay at the top of their project.

Press `?` for the shortcut dialog. `cade --help` lists the command-line options.

The tables below are the output of `cade --keys`; regenerate them from there when shortcuts change.

### Sidebar

| Key | Action |
|---|---|
| `j/k` | move down / up (also ↓/↑) |
| `⇧↑/⇧↓` | previous / next project |
| `l/⏎` | open session (click works too; new one on a project row) |
| `n` | new session in this project |
| `/` | fuzzy search session titles, all projects |
| `←/→` | collapse / expand project (`→` on a session opens it) |
| `a` | add project (name, then directory; ⇥ opens a match) |
| `⇧PgUp/Dn` | move project up / down |
| `r` | rename session (empty = Claude's title) |
| `s/*` | star / unstar session; starred go first (click ★ too) |
| `d/dd` | d: delete session from disk · dd: remove project from list (both ask) |
| `g/G` | top / end |
| `⇥` | go to Claude pane |
| `,` | settings: theme, hints, sidebar side, edit config.yaml in $EDITOR |
| `?` | this help |
| `q` | quit (asks while sessions run) |
| `H` | show / hide these hints |

### Claude pane

| Key | Action |
|---|---|
| `Ctrl+Esc` | switch sidebar ↔ pane (also F12) |
| `F3` | git diff of the session's project, sidebar too; s: side by side ↔ unified |
| `F2` | one shell for the app, started in the project's directory, over the pane, sidebar too; again goes back |
| `Ctrl+F` | focus next waiting session (◆), oldest first, sidebar too |
| `F4/^Z` | hide / show the sidebar (Ctrl+Z too, except in the shell), sidebar too |
| `Ctrl+⇧Q` | quit cade, sidebar too (asks while sessions run) |
| `⇧PgUp/Dn` | scroll back / forward (PgUp/PgDn too) |
| `wheel` | scroll; or drag the scrollbar |
| `drag` | select text, 2×click word, 3×click line → copied |
| `other keys` | sent to Claude as-is |

Mouse wheel and the scrollbar scroll the list when it doesn't fit.

Open sessions keep running when you switch to another one; `●` marks them. Copying uses OSC 52, so
your terminal must allow clipboard writes from programs. If Claude turns on mouse reporting, hold
`Shift` to select.

When a session other than the one you're typing in needs you (Claude rings the bell on a
permission prompt or when it waits for input), cade rings your terminal's bell, sends a desktop
notification, marks the session `◆` with its title in bold, and counts waiting sessions in the
pane's title bar. `Ctrl+F` opens the one that has waited longest, from the sidebar or the Claude
pane; press it again for the next. cade starts Claude with
`--settings '{"preferredNotifChannel":"terminal_bell"}'` for this.

## Configuration

All files live in `~/.config/cade/` (`$XDG_CONFIG_HOME/cade/` if set; on macOS
`~/Library/Application Support/cade/`).

| File | Written by | Contents |
|---|---|---|
| `projects.json` | cade | projects, their order and collapsed state, session titles and stars |
| `state.json` | cade | remembered UI state (hints hidden, sidebar left or right, diff side by side or unified) |
| `config.yaml` | you (cade writes only `theme`) | theme, diff, toasts, Claude accounts |

### Themes

Dracula is the default. Built-in themes are `dracula`, `terminal` (your terminal's own colours), `tokyonight-night`,
`tokyonight-day`, `catppuccin-mocha`, `catppuccin-latte`, `gruvbox-dark`, `gruvbox-light`, `nord`, `solarized-dark`
and `solarized-light`.

```yaml
theme: dracula
```

Define your own; fields you leave out come from Dracula:

```yaml
theme: mine
themes:
  mine: { key: "#50fa7b", titleBg: "#ff79c6" }
```

Colour fields, as `#rrggbb`: `bg`, `fg`, `muted`, `project`, `selBg`, `selFg`, `key`, `running`,
`attention`, `thumb`, `error`, `titleBg`, `titleFg`. A bad config falls back to Dracula and shows the error in
a toast. The config is read at startup.

### Diff

`F3` shows `git diff` of the session's project over the Claude pane, side by side; `s` switches to
unified and back, and cade remembers the choice. It reloads every 2 seconds and keeps your scroll
position. Long lines wrap inside their column; to cut them with `…` instead:

```yaml
diff:
  wrap: false
```

### Toasts

Success, notice, warning and error messages show as toasts that stack in a corner and fade on
their own (3s for success, up to 8s for errors). Top right by default; also `top-left`,
`bottom-right` and `bottom-left`:

```yaml
toasts:
  position: bottom-right
```

### Settings dialog

`,` opens settings. Picking a theme applies at once and writes the `theme` key to `config.yaml`;
the rest of the file, comments included, is kept (indentation is normalised to two spaces).
"Sidebar" puts the sessions sidebar on the left (default) or right; it is saved in `state.json`.
"Edit config.yaml" suspends cade and opens the file in `$VISUAL`, else `$EDITOR`, else `vi`. On
save cade checks the file: a clean one takes effect at once (account changes apply to sessions
started after it), otherwise the dialog lists the issues and the config from before the edit stays
in use. Bells from Claude while the editor is open are missed.

`cade --check` lists every problem in `config.yaml` (unknown keys with a "did you mean", a bad
`default`, programs not on `PATH`, a bad theme, missing `configDir` and `dirs` directories) and
exits 1 if there are any. Themes colour cade's own panels only; the Claude pane
uses Claude's `/theme`.

`config.json` from older versions is no longer read. JSON is valid YAML, so renaming it to
`config.yaml` is enough.

### Claude accounts

Without a `claude` section, cade runs plain `claude`. Accounts let you run claude through a wrapper
such as firejail, or use more than one Claude login:

```yaml
claude:
  default: personal          # may be left out when there is one account
  accounts:
    personal:
      command: [firejail, --quiet, --read-only=/, --read-write=$PWD,
                --profile=$HOME/.config/firejail/claude.profile,
                claude, --dangerously-skip-permissions]
      env: { ANTHROPIC_BASE_URL: "http://localhost:8787" }
    work:
      command: [claude-firejail.sh]
      configDir: ~/.claude-work
      dirs: [~/www/client-a, ~/www/client-b]
```

| Field | Meaning |
|---|---|
| `command` | program and flags; cade appends `--session-id <id>` or `--resume <id>`. Default `claude` |
| `configDir` | passed as `CLAUDE_CONFIG_DIR`; cade reads this account's sessions from it. Default `~/.claude` |
| `env` | extra environment variables |
| `dirs` | projects in these directories use this account; the deepest match wins, others use `default` |

Values may use a leading `~`, `$VAR` and `$PWD` (the project directory). No shell runs, so `~` in
the middle of a flag is not expanded: use `$HOME`. cade refuses to start if the file is invalid
(unknown fields included) or an account's program is not on `PATH`, so it never falls back to
plain `claude` quietly. A bad theme or a missing directory doesn't stop it; `cade --check` lists those too.

## Development

| Command | Does |
|---|---|
| `make build` | compile `bin/cade` (static) |
| `make test` | run tests |
| `make vet` / `make fmt` | static checks / format |
| `make tidy` | tidy `go.mod` |
| `make shell` | shell in the dev container |
| `make image` | build the production image |
| `make clean` | remove build output and the cache volume |
| `make bump` | bump the version: `PART=patch\|minor\|major` (default patch) or `V=x.y.z` |

All Go commands run in Docker through `compose.yaml`. `make bump` runs on the host and needs GNU
`sed`.

### Layout

| Path | Contents |
|---|---|
| `cmd/cade/main.go` | app: event loop, sidebar, Claude pane, dialogs |
| `cmd/cade/sessions.go` | reading sessions and titles from `~/.claude/projects` |
| `cmd/cade/projects.go` | project list and state files |
| `cmd/cade/theme.go` | themes |
| `cmd/cade/config.go` | `config.yaml` and Claude accounts |
| `third_party/vaxis` | vendored [Vaxis](https://git.sr.ht/~rockorager/vaxis) v0.17.1 |

Vaxis is vendored (via a `replace` in `go.mod`) for one added file,
`widgets/term/scrollinfo.go`, which exposes the terminal widget's scroll position for the
scrollbar. To upgrade Vaxis, copy the new release into `third_party/vaxis` and keep that file.

## Known limits

- Title truncation counts characters, not display width, so wide characters (CJK, emoji) can
  misalign a row.
- If you switch sessions inside a pane with `/resume` or `/clear`, the sidebar still shows the
  session the pane was opened with.
- `Ctrl+Esc` needs the kitty keyboard protocol; without it use `F12`.
