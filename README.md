# hnet

Phase-1 prototype for a mihomo-based proxy manager.

What it does now:

- `hnetd` runs as a local daemon over a Unix socket.
- `hnet` is a small TUI for importing, switching, and deleting provider subscriptions.
- `hnetd` hands the subscription URL directly to `mihomo` via `proxy-providers`, writes a small wrapper `config.yaml`, and keeps `mihomo` running.
- node selection, latency/speed testing, and system proxy toggling.
- rule-based routing with local/direct, China direct, and global proxy fallbacks.

What it does not do yet:

- TUN mode
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
make install
make uninstall
make test
make vet
make fmt
make verify
make clean
```

## Install

Install both binaries into `~/.local/bin`:

```bash
make install
```

If `~/.local/bin` is not already in your `PATH`, add this to your shell profile:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Then reload your shell and run `hnet` / `hnetd` directly.

## Run

Start the daemon:

```bash
hnetd start
```

Restart the daemon:

```bash
hnetd restart
```

Open the TUI:

```bash
hnet
```

Type the subscription URL given by your node provider and press `Ctrl+S` to import it.

Useful files:

- daemon socket: `~/Library/Application Support/hnet/hnetd.sock`
- daemon log: `~/Library/Application Support/hnet/hnetd.log`
- mihomo config: `~/Library/Application Support/hnet/runtime/config.yaml`
- provider cache: `~/Library/Application Support/hnet/runtime/providers/`
- mihomo log: `~/Library/Application Support/hnet/runtime/mihomo.log`

Stop the daemon:

```bash
hnetd stop
```
