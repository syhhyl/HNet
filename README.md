# hnet

Phase-1 prototype for a mihomo-based proxy manager.

What it does now:

- `hnetd` runs as a local daemon over a Unix socket.
- `hnet` is a small TUI for importing a real provider subscription URL.
- `hnetd` hands the subscription URL directly to `mihomo` via `proxy-providers`, writes a small wrapper `config.yaml`, and keeps `mihomo` running.

What it does not do yet:

- system proxy
- TUN mode
- node selection UI
- launchd integration

## Requirements

- Go 1.25+
- `mihomo` installed and available in `PATH`

On macOS with Homebrew:

```bash
brew install mihomo
```

## Build

Quick one-command build:

```bash
make build
```

`make` also works because `build` is the default target.

Manual build:

```bash
go build -o ./hnet ./cmd/hnet
go build -o ./hnetd ./cmd/hnetd
```

Useful helper targets:

```bash
make test
make vet
make fmt
make verify
make clean
```

## Run

Start the daemon:

```bash
./hnetd start
```

Open the TUI:

```bash
./hnet
```

Type the subscription URL given by your node provider and press `Enter`.

Useful files:

- daemon socket: `~/Library/Application Support/hnet/hnetd.sock`
- daemon log: `~/Library/Application Support/hnet/hnetd.log`
- mihomo config: `~/Library/Application Support/hnet/runtime/config.yaml`
- provider cache: `~/Library/Application Support/hnet/runtime/providers/imported.yaml`
- mihomo log: `~/Library/Application Support/hnet/runtime/mihomo.log`

Stop the daemon:

```bash
./hnetd stop
```
