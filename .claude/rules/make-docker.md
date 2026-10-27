# Commands

- Run every command through a `make` target; targets run inside the dockerised environment (`compose.yaml`, `Dockerfile`).
- Never run toolchain commands (`go build`, `go test`, `gofmt`, ...) directly on the host.
- Need a command with no target? Add a `make` target that runs it in the container, then use it. `make help` lists targets.
