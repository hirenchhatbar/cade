# Cade

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
selected session. Cade is only a wrapper: every key, paste and mouse event in the Claude pane goes
to Claude unchanged, so Claude's own shortcuts and commands keep working.

```
 SESSIONS               Cade v0.1.0 │ cade › fix login flow (34b6e4a0)
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

Run Cade on the host, not inside the container: it needs your `claude` binary and `~/.claude`.

## Usage

On first start Cade adds the directory you launched it from as a project. Add more with `a`.
Sessions are read from `~/.claude/projects` and refreshed every 2 seconds. A session's title is the
one set in Cade with `r` (`Ctrl+T` in that dialog asks Claude for one from the first prompts), else its `/rename` name, else its agent name, else Claude's AI title,
else its first prompt. Starred sessions (`s`, `*` or a click on the ★ column) stay at the top of their project.
A project's name and tree lines use the theme's `project` colour. `c` gives the project its own colour, the
one farthest from the other projects' colours (about 50 projects stay apart), and keeps it in `projects.json`;
`c` again turns it round the wheel. `C` goes back to the theme's colour.

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
| `←/→` | collapse / expand project (→ on a session opens it) |
| `a` | add project (name, then directory; ⇥ opens a match) |
| `⇧PgUp/Dn` | move project up / down |
| `r` | rename session (empty = Claude's title, ^T generates one) |
| `c/C` | color this project (name and tree lines), again turns it round the wheel / back to the theme's, kept |
| `s/*` | star / unstar session; starred go first (click ★ too) |
| `d/dd` | d: delete session from disk · dd: remove project from list (both ask) |
| `g/G` | top / end |
| `⇥` | go to Claude pane |
| `-/+` | narrower / wider sidebar (or drag the line beside it), remembered |
| `,` | settings: theme, status bar and its height, sidebar side and width, shell, diff and editor size, editor command, edit config.yaml in $EDITOR |
| `?` | this help |
| `q` | quit (asks while the shell or editor runs) |
| `S` | show / hide the status bar, remembered; its height in , settings |


### Claude pane


| Key | Action |
|---|---|
| `Ctrl+Esc` | switch sidebar ↔ pane (also F12) |
| `F2` | one shell for the app, started in the project's directory, split under the pane, sidebar too; again hides it |
| `F3` | git diff of the session's project, split under it, sidebar too; again closes it; s: side by side ↔ unified; ^D/^U: half page |
| `F4` | one editor for the app (editor.cmd in , settings), started in the project's directory, split under the pane in the shell's place, sidebar too; again hides it |
| `F5/^Z` | hide / show the sidebar (Ctrl+Z too, except in the shell), sidebar too |
| `Ctrl+N` | new session in the open session's project, else the cursor's, sidebar too; not in the shell or editor |
| `Ctrl+F` | focus next waiting session (◆), oldest first, sidebar too; not in the editor |
| `^⇥` | go to sidebar, cursor to the session opened before (cycles, latest first, kept across restarts), sidebar too |
| `Ctrl+⇧Q` | quit Cade, sidebar too (asks while the shell or editor runs) |
| `^↑/^↓` | go to sidebar, cursor up / down, sidebar too |
| `^⇧↑/^⇧↓` | go to sidebar, previous / next project, sidebar too |
| `⌥↑/⌥↓` | with the diff, shell or editor split with the session: focus the session / the diff, shell or editor |
| `⌥⇧←/→` | move the split line: ← / ↑ grows the diff, shell or editor, → / ↓ shrinks it, saved to config.yaml; or drag the line |
| `F6` | diff, shell or editor beside ↔ under the session, remembered; under while the pane is under 121 columns |
| `F11` | focused window fills the pane ↔ split: the session (diff, shell or editor stays open), or the diff, shell or editor (remembered); split sizes in , settings |
| `Ctrl+R` | rename this session (goes to the sidebar) |
| `Ctrl+D` | delete this session from disk (asks) |
| `⇧PgUp/Dn` | scroll back / forward (PgUp/PgDn too) |
| `wheel` | scroll; or drag the scrollbar |
| `drag` | select text, 2×click word, 3×click line → copied |
| `other keys` | sent to Claude as-is |

Mouse wheel and the scrollbar scroll the list when it doesn't fit.

Open sessions keep running when you switch to another one; `●` marks them. A split shell, editor
or diff stays open under the session you switch to; the diff follows its project. Copying uses OSC 52, so
your terminal must allow clipboard writes from programs. If Claude turns on mouse reporting, hold
`Shift` to select.

When a session other than the one you're typing in needs you (Claude rings the bell on a
permission prompt or when it waits for input), Cade rings your terminal's bell, sends a desktop
notification, marks the session `◆` with its title in bold, and counts waiting sessions in the
pane's title bar. `Ctrl+F` opens the one that has waited longest, from the sidebar or the Claude
pane; press it again for the next. Cade starts Claude with
`--settings '{"preferredNotifChannel":"terminal_bell"}'` for this.

## Configuration

All files live in `~/.config/cade/` (`$XDG_CONFIG_HOME/cade/` if set; on macOS
`~/Library/Application Support/cade/`).

| File | Written by | Contents |
|---|---|---|
| `projects.json` | Cade | projects, their order, collapsed state and colour (`#rrggbb`, hand-editable), session titles and stars |
| `state.json` | Cade | remembered UI state (status bar hidden, sidebar left or right and its width, diff side by side or unified, diff, shell and editor full or split, split beside or under, opened sessions latest first: cursor starts on last one) |
| `config.yaml` | you (Cade writes only `theme`, `statusBar.height`, `quit.confirm`, `shell.height`, `diff.height`, `editor.cmd` and `editor.height`) | theme, diff, status bar, toasts, quit, shell, editor, Claude accounts |

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

`F3` shows `git diff` of the session's project split under the session, like the `F2` shell, in its
place: `F3` hides the shell and `F2` the diff. Keys scroll the diff while it has focus; `⌥↑/⌥↓`
move the focus to the session and back, `F11` makes the diff full height and back (remembered on
its own), and a drag selects and copies, side by side only from the half it started in. With no
session open, the diff fills the pane. It is side by side; `s` switches to unified and back, and
Cade remembers the choice. It reloads every 2 seconds and keeps the scroll position. Long lines
wrap inside their column; to cut them with `…` instead, or to set its height (50% by default,
from 10 to 90):

```yaml
diff:
  wrap: false
  height: 40
```

### Status bar

The bar along the bottom shows Cade's name and version, then, right-aligned, the keys for whatever has focus: the
sidebar, the Claude pane, the diff or search. A dialog shows its keys on its own last row, so
the bar shows none while one is open. `S` in the sidebar hides and shows
it; Cade remembers the choice. It takes as many rows as the longest list of keys needs at the
window's width, so it keeps its height as focus moves. To fix it at 1 to 5 rows instead, cutting
keys that don't fit with `…` (`[?] help` comes first, so it stays):

```yaml
statusBar:
  height: 1
```

### Toasts

Success, notice, warning and error messages show as toasts that stack in a corner and fade on
their own (3s for success, up to 8s for errors). Top right by default; also `top-left`,
`bottom-right` and `bottom-left`:

```yaml
toasts:
  position: bottom-right
```

### Quit

`q` and `Ctrl+⇧Q` ask before quitting while the shell or editor runs, since quitting closes
them. Claude sessions keep running in the session server (below) and never make Cade ask.
To quit at once:

```yaml
quit:
  confirm: false
```

### Session server

Claude sessions run in a session server, `cade --serve`, not in Cade itself. Cade starts the
server the first time you open a session, and the server keeps running after Cade quits. Start
Cade again, on this machine or over SSH (from a phone, for example), and it shows the running
sessions again (● in the sidebar), with their screen and scrollback.

- The server listens on `$XDG_RUNTIME_DIR/cade.sock` (else `/tmp/cade-<uid>.sock`), readable
  only by you. It never listens on the network; reach it from another device over SSH.
- More than one Cade can show the same session. The Cade that last typed, clicked or focused the
  session sets its size, and the screen reflows to it.
- Deleting a session (`d`, `Ctrl+D`) or removing its project ends it in the server too.
- The shell and editor still run inside Cade and close when it quits.
- Stopping the server (for example `pkill -f 'cade --serve'`) ends all running sessions; their
  transcripts stay on disk and can be resumed. After an upgrade, Cade warns while the old server
  runs; restart it to use the new version.

#### Run the server as a systemd service

Without a service, the server stops when your machine restarts, and it can stop when you log out.
A systemd user service starts it at boot and keeps it running without a login.

1. Put the binary in a fixed place. The service runs it from there:

   ```sh
   make build
   mkdir -p ~/.local/bin
   cp bin/cade ~/.local/bin/cade
   ```

2. Create the service file:

   ```sh
   mkdir -p ~/.config/systemd/user
   cat > ~/.config/systemd/user/cade.service <<'EOF'
   [Unit]
   Description=Cade session server

   [Service]
   ExecStart=%h/.local/bin/cade --serve
   Restart=on-failure

   [Install]
   WantedBy=default.target
   EOF
   ```

   `%h` is your home directory. If you put the binary in a different place, change `ExecStart`.

3. Stop a server that Cade started on its own. Only one server can run, so the service fails while
   it runs. This step ends the sessions in it:

   ```sh
   pkill -f 'cade --serve'
   ```

4. Load the service, then start it now and at each boot:

   ```sh
   systemctl --user daemon-reload
   systemctl --user enable --now cade.service
   ```

5. Let your user services run when you are not logged in:

   ```sh
   loginctl enable-linger "$USER"
   ```

6. Check that the server runs:

   ```sh
   systemctl --user status cade.service
   ```

To read its log, run `journalctl --user -u cade.service`. To upgrade, copy the new binary to
`~/.local/bin/cade`, then run `systemctl --user restart cade.service`. A restart ends the running
sessions. To remove the service, run `systemctl --user disable --now cade.service`, then delete
the file.

### Shell

`F2` opens the shell split under the session, so you can copy from Claude while typing in the
shell: a drag in either half selects and copies, and keys go to the half that has focus.
`⌥↑/⌥↓` move the focus between them. `F11` makes the focused half fill the pane and back:
on the shell Cade remembers the choice; on the session the shell stays open behind it until
the next `F11`. Some terminals keep `F11` for their own fullscreen; turn that off there.
The shell takes 50% of the pane by default, from 10 to 90 (`height` is the width side by side):

```yaml
shell:
  height: 40
```

### Split layout

The diff, shell and editor split the pane with the session: under it by default. `F6` puts them
beside it, after a line, and back; Cade remembers the choice in `state.json`. Side by side needs
a pane at least 121 columns wide, 60 for each half; a narrower pane stacks them until it is wide
enough. `⌥⇧←`/`⌥⇧↑` grow the diff, shell or editor by 10% and `⌥⇧→`/`⌥⇧↓` shrink it; dragging
the line between them (side by side) or the lower title bar (stacked) sets any size. Both write
`diff.height`, `shell.height` or `editor.height` to `config.yaml`.

### Editor
`F4` runs one editor for the app, started in the cursor's project directory, split under the
session like the `F2` shell, in its place: `F4` hides the shell, `F2` the editor and `F3` either.
`F4` again hides it, still running. `⌥↑/⌥↓` move focus between session and editor, and `F11`
makes the editor full height and back (remembered on its own). The command runs through `sh`,
so flags work; the default is `$VISUAL`, else `$EDITOR`, else `vi`. The editor takes 50% of the
pane by default, from 10 to 90:

```yaml
editor:
  cmd: nvim .
  height: 70
```

Keys go to the editor except Cade's own (`Ctrl+Esc`, `F2`–`F6`, `F11`, `F12`, `Ctrl+Z`,
`Ctrl+⇧Q`, `^↑/^↓`, `^⇧↑/^⇧↓`, `F6`, `⌥⇧` arrows); `Ctrl+F` stays with the editor. Quitting Cade closes
the editor without saving.

### Settings dialog

`,` opens settings. Picking a theme applies at once and writes the `theme` key to `config.yaml`;
the rest of the file, comments included, is kept (indentation is normalised to two spaces).
"Confirm quit" switches `quit.confirm`, written to `config.yaml` the same way.
"Status bar (S)" shows or hides the status bar, like `S`; it is saved in `state.json`.
"Status bar height" steps `statusBar.height` from auto (0) to 5 rows, written to `config.yaml` the same way.
"Sidebar" puts the sessions sidebar on the left (default) or right; it is saved in `state.json`.
"Sidebar width" steps the sidebar width by 2 columns (default 42, at least 30, leaving the pane at least 30); `-`/`+` in the sidebar and dragging the line between sidebar and pane do the same. It is saved in `state.json`.
"Shell size", "Diff size" and "Editor size" step `shell.height`, `diff.height` and `editor.height` by 10%, written to `config.yaml` the same way.
"Editor (F4)" edits `editor.cmd` as text: `⏎` saves it to `config.yaml` the same way (empty = `$VISUAL`, else `$EDITOR`), `Esc` cancels; a running editor keeps its old command.
"Edit config.yaml" suspends Cade and opens the file in `$VISUAL`, else `$EDITOR`, else `vi`. On
save Cade checks the file: a clean one takes effect at once (account changes apply to sessions
started after it), otherwise the dialog lists the issues and the config from before the edit stays
in use. Bells from Claude while the editor is open are missed.

`cade --check` lists every problem in `config.yaml` (unknown keys with a "did you mean", a bad
`default`, programs not on `PATH`, a bad theme, missing `configDir` and `dirs` directories) and
exits 1 if there are any. Themes colour Cade's own panels only; the Claude pane
uses Claude's `/theme`.

`config.json` from older versions is no longer read. JSON is valid YAML, so renaming it to
`config.yaml` is enough.

### Claude accounts

Without a `claude` section, Cade runs plain `claude`. Accounts let you run claude through a wrapper
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
| `command` | program and flags; Cade appends `--session-id <id>` or `--resume <id>`. Default `claude` |
| `configDir` | passed as `CLAUDE_CONFIG_DIR`; Cade reads this account's sessions from it. Default `~/.claude` |
| `env` | extra environment variables |
| `dirs` | projects in these directories use this account; the deepest match wins, others use `default` |

Values may use a leading `~`, `$VAR` and `$PWD` (the project directory). No shell runs, so `~` in
the middle of a flag is not expanded: use `$HOME`. Cade refuses to start if the file is invalid
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
| `cmd/cade/serve.go` | session server (`cade --serve`) and its protocol |
| `cmd/cade/remote.go` | the pane's view of a session the server runs |
| `cmd/cade/sessions.go` | reading sessions and titles from `~/.claude/projects` |
| `cmd/cade/projects.go` | project list and state files |
| `cmd/cade/theme.go` | themes |
| `cmd/cade/config.go` | `config.yaml` and Claude accounts |
| `third_party/vaxis` | vendored [Vaxis](https://git.sr.ht/~rockorager/vaxis) v0.17.1 |

Vaxis is vendored (via a `replace` in `go.mod`) for one added file,
`widgets/term/scrollinfo.go`, which exposes the terminal widget's scroll position for the
scrollbar, and for `EventClipboard` (`widgets/term/events.go`, `osc.go`), which hands a copy
from a headless terminal in the session server to Cade. To upgrade Vaxis, copy the new release
into `third_party/vaxis` and keep both changes.

## Known limits

- Title truncation counts characters, not display width, so wide characters (CJK, emoji) can
  misalign a row.
- If you switch sessions inside a pane with `/resume` or `/clear`, the sidebar still shows the
  session the pane was opened with.
- `Ctrl+Esc` needs the kitty keyboard protocol; without it use `F12`.
- The session server has no host terminal, so Claude's colour queries (OSC 10/11) go
  unanswered and it falls back to its defaults. Terminal images in a session are not shown.
