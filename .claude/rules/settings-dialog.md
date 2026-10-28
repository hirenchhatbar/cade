# Settings dialog

- Adding a `config.yaml` key or a remembered UI state? Ask the user whether it gets a row in the `,` settings dialog (`settingRows` in `cmd/cade/keys.go`, drawn in `drawSettings`). Write the value with `saveConfig` (config) or `state.json` (UI state), and update the README "Settings dialog" section.
