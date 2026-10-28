# Tech stack

| Area | Choice |
|------|--------|
| Language | Go 1.27.3, module `cade` |
| TUI | [vaxis](https://go.rockorager.dev/vaxis) v0.17.1, vendored fork in `third_party/vaxis` via `replace` in `go.mod` |
| Config | `gopkg.in/yaml.v3` for `config.yaml` |
| Tests | Go `testing` (`make test`) |
| Dev environment | Docker Compose service `go`, image target `dev` (`golang:1.27.3`), run as host user |
| Build | `make build`: static binary `bin/cade` (`CGO_ENABLED=0`) |
| Production image | Multi-stage `Dockerfile`, final image `gcr.io/distroless/static-debian12:nonroot` |
| Task runner | `Makefile`; `make help` lists targets |
| Runtime | Linux, `claude` CLI on `PATH`, terminal with kitty keyboard protocol and 24-bit colour |
