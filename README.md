# mcbot

A Go stress-testing tool for Minecraft Java Edition (1.21.x) servers. It floods
a server with many fake offline-mode clients that join, stay connected, and
wander around spawn — useful for measuring login throughput, player-cap
behaviour, and server stability under load.

Built on [Tnze/go-mc](https://github.com/Tnze/go-mc) (protocol 767 / MC 1.21).

## Usage

```
go build -o mcbot .
./mcbot -address 127.0.0.1:25565 -bots 200 -rate 50
```

### Flags

| Flag            | Default            | Description                                        |
|-----------------|--------------------|----------------------------------------------------|
| `-address`      | `127.0.0.1:25565`  | Server address `host:port`                         |
| `-bots`         | `100`              | Total number of bots to spawn                      |
| `-rate`         | `20`               | Bots spawned per second (`0` = all at once)        |
| `-stagger`      | `0`                | ms between bot joins, overrides `-rate`            |
| `-prefix`       | ``                  | Bot name prefix; empty uses realistic gamer-style generated names (e.g. `xX_ShadowBlade_Xx`) |
| `-names-file`   | ``                  | Write the generated usernames (one per line) to this file |
| `-move`         | `true`             | Make bots wander around spawn                      |
| `-tick`         | `50`               | Movement tick in ms                                |
| `-radius`       | `10`               | Movement radius around spawn (blocks)              |
| `-join-timeout` | `30`               | Seconds per bot to complete login                  |
| `-duration`     | `0`                | Stop after N seconds (`0` = until Ctrl+C)          |
| `-interval`     | `1`                | Status line refresh interval (seconds)             |
| `-quiet`        | `false`            | Suppress per-bot log lines                         |
| `-user`         | `User7364`         | Auth username for `/register` and `/login`         |
| `-pass`         | `User7364`         | Auth password for `/register`                      |
| `-register`     | `true`             | Run `/cracked` + `/register` on first login; on reconnects run `/login` (`false` = always `/login`) |
| `-auth-delay`   | `500`              | ms to wait after login before sending auth commands|
| `-reconnect`    | `0`                | Seconds to wait before reconnecting a dropped bot (`0` = no reconnect) |
| `-max-reconnects`| `0`               | Max reconnect attempts per bot (`0` = unlimited)   |
| `-web`          | ``                 | Start the web UI on this address (e.g. `:8080`)  |
| `-decorate`     | `false`            | Make the server look populated: bots join, stay idle, and stay online (reconnect defaults to 10s) |
| `-hide`         | `false`            | With `-decorate`, fly bots above the build limit so players can't see them |

## Web UI

```
./mcbot -web :8080
```

Opens a browser UI at `http://localhost:8080` where you can configure a run
from a form (address, bot count, rate/stagger, auth, reconnect, movement, ...),
start/stop it, and watch live stats. The same flags above become the page's
default values.

API endpoints:
- `GET  /`            — the UI
- `POST /api/start`   — start a run (JSON config, see below)
- `POST /api/stop`    — stop the current run
- `GET  /api/stats`   — live JSON snapshot

`/api/start` accepts the same fields as the CLI flags, e.g.:

```json
{
  "address": "127.0.0.1:25565",
  "bots": 200,
  "rate": 50,
  "stagger": 0,
  "duration": 0,
  "user": "User7364",
  "pass": "User7364",
  "register": true,
  "move": true
}
```

## Output

Every second it prints a status line, e.g.:

```
[2s] 127.0.0.1:25565 | in-game: 194/200 | joined: 200 | failed: 0 | dropped: 6 | success: 100.0%
```

Press Ctrl+C (or hit `-duration`) to stop; a final summary is printed.

## Making your server look busier

Use `-decorate` to pad the player count with real connections that stay online:

```
./mcbot -decorate -address my.server.com:25565 -bots 150 -stagger 1000 -names-file names.txt
```

- Bots join at `-rate`/`-stagger`, sit idle (no wandering), and auto-reconnect if
  they drop, so the player list stays full.
- Add `-hide` to fly them above the build limit — they still count toward the
  player list and `/list`, but nobody sees them in the world:

  ```
  ./mcbot -decorate -hide -address my.server.com:25565 -bots 150
  ```

- Tip: use a slow `-stagger` (e.g. 1000-3000ms) so players trickle in instead of
  appearing all at once, which looks more natural.

## Notes

- Requires a server running in **offline mode** (`online-mode=false`).
- With `-register` (default), each bot's first login sends `/cracked` then
  `/register <user> <pass>`; once registered it switches to `/login <user>` on
  reconnects, so bots stay logged in across disconnects.
- Bots hold their Y coordinate after spawn, so they'll hover rather than fall
  through the world — good enough for load testing.
- To also hammer the server with churn, run short `-duration` runs back to back.
