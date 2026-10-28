# Directory structure

```
.
├── cmd/cade/            # the app: one `main` package, tests beside each file
│   ├── main.go          # startup, event loop, layout, Claude pane
│   ├── config.go        # config.yaml and Claude accounts
│   ├── keys.go          # key bindings: single source for handling, hints and help dialog
│   ├── tree.go          # sidebar model: projects, sessions, cursor, scroll
│   ├── projects.go      # project list, titles, starred sessions
│   ├── sessions.go      # Claude session files: read, list, delete
│   ├── diff.go          # F3 overlay: git diff of a project
│   ├── fuzzy.go         # fuzzy filter for search
│   ├── theme.go         # themes and palettes
│   ├── toast.go         # toast messages
│   └── draw.go          # drawing helpers
├── third_party/vaxis/   # vendored fork of go.rockorager.dev/vaxis (TUI library), wired in by `replace` in go.mod
├── docs/                # project documentation
├── .claude/rules/       # rules imported by CLAUDE.md
├── Makefile             # every command runs through a target; `make help` lists them
├── Dockerfile           # dev and production images
├── compose.yaml         # dockerised dev environment
├── go.mod, go.sum       # Go module `cade`
└── bin/                 # build output (git-ignored)
```
