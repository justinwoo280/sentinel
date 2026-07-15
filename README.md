# Sentinel

A single-binary tool for maintaining the geographic/reputation standing of VPS
IP addresses. It keeps an IP "warm" against services (Google, high-reputation
sites) and periodically runs a full IP-quality check, all coordinated from a
central Master over an encrypted reverse control connection.

Sentinel is a clean-room Go rewrite of the original bash/python `IP-Sentinel`
project: one static binary, no runtime script downloads, no inbound ports on
edge nodes, and an end-to-end encrypted control channel.

## Architecture

```
                Telegram Bot  <->  User
                     |
                  [ Master ]  <-- EWP/v2.1 encrypted TCP :8443 (only port to open)
                   /   |   \
              (agents dial OUT — no inbound ports needed)
             /         |         \
        [ Agent ]  [ Agent ]  [ Agent ]   ... edge VPS nodes
```

- **Master** — control plane. Runs a Telegram bot (long-polling, no public
  domain/cert needed) and an EWP/v2.1 server. Holds a SQLite store of nodes and
  IP-trend history.
- **Agent** — edge node. Dials *out* to the Master over an
  X25519+ML-KEM-768 handshake with per-frame ChaCha20-Poly1305 AEAD, then keeps
  a persistent connection for commands. Runs three modules:
  - **Google keepalive** — human-like Google/YouTube visits + location probing.
  - **Reputation warmup** — visits regional high-reputation sites.
  - **IP quality check** — a full fraud-score / streaming-unblock / mail-server
    report (see [IP quality API keys](#ip-quality-api-keys)).

Only the Master needs an open inbound port. Agents are pure outbound and work
behind NAT/CGNAT.

## Install

The install script downloads the release binary, **verifies its SHA-256 against
the release checksum list**, installs it to `/usr/local/bin/sentinel`, and
launches the interactive installer.

```sh
# Agent (edge node)
curl -fsSL https://raw.githubusercontent.com/justinwoo280/sentinel/main/scripts/install.sh | sh -s -- --role agent

# Master (control plane)
curl -fsSL https://raw.githubusercontent.com/justinwoo280/sentinel/main/scripts/install.sh | sh -s -- --role master

# Or run without --role to be prompted:
curl -fsSL https://raw.githubusercontent.com/justinwoo280/sentinel/main/scripts/install.sh | sh
```

Manual install: download `sentinel-linux-<amd64|arm64>` and `checksums.txt` from
the [releases page](https://github.com/justinwoo280/sentinel/releases), verify,
then run `sentinel install`.

Requires root (writes `/usr/local/bin`, `/etc/sentinel`, `/var/lib/sentinel`).

## Deployment

### 1. Master (do this first)

```sh
# Generate the static keypair + config and install the systemd service.
sentinel master init --service
```

This:
1. generates the X25519 static keypair at `/var/lib/sentinel/master_static.key`
   (mode `0600`; re-runs preserve the existing key),
2. writes `/etc/sentinel/master.yaml`,
3. prints the **static public key** — copy this; every agent needs it,
4. installs and enables `sentinel-master.service` (or an `@reboot` cron entry on
   non-systemd hosts).

Then add your Telegram bot token **and your admin allowlist**, and start:

```sh
sentinel manage --role master     # menu -> Edit configuration -> Telegram token + Admin IDs
systemctl restart sentinel-master
```

> **Important — bot authorization.** The Telegram bot is **fail-closed**: only
> Telegram user IDs on the `telegram.admin_ids` allowlist can operate it. If the
> list is empty, the bot denies everyone. You **must** add at least one admin ID.
> To find your ID, just message the bot — it replies with your numeric ID (which
> you then add to the allowlist). Config example:
>
> ```yaml
> telegram:
>   token: "123456:ABC-your-bot-token"
>   admin_ids:
>     - 123456789      # your Telegram user ID
>   enable_ota: true
> ```

Open inbound TCP on the control port (default `:8443`) in your firewall / cloud
security group. This is the only port Sentinel needs open, anywhere.

### 2. Agent (per edge node)

```sh
sentinel install --role agent
```

The interactive installer asks for:
- **Region** (JP/US/HK/KR/SG/DE/UK/CA/AU),
- **Master EWP address** (`host:port`),
- **Master static public key** (the base64 string from step 1),
- **Alias** (display name, optional).

It generates a UUID + node name, writes `/etc/sentinel/agent.yaml`, prints a
`SENTINEL-REG:...` registration blob, and installs `sentinel-agent.service`.

### 3. Register the agent (out-of-band, one time)

Paste the `SENTINEL-REG:...` blob directly to your Master Telegram bot. The
Master validates it, adds the UUID to its whitelist, and stores the node. Then
start the agent:

```sh
systemctl enable --now sentinel-agent
```

The agent dials the Master, sends `hello`, and begins its keepalive schedule.
Manage everything else from the Telegram bot (`/start`).

## Management panel

`sentinel manage` is an interactive TUI for whichever role is installed
(auto-detected; use `--role agent|master` to force):

- **Show status & config summary** — service state + config at a glance
- **Start / Stop / Restart / Enable on boot** — service control
- **Edit configuration** — alias, module toggles, Master address/key, OTA,
  schedule, bind IP, and quality API keys (agent); Telegram token, **admin
  allowlist**, OTA, listen address (master)
- **Regenerate registration blob** (agent) / **Show public key** (master)
- **View recent logs** — journalctl or the cron log file
- **Uninstall** — stop + remove the service, optionally the config, data
  directory, and binary

## IP quality API keys

The quality module aggregates many data sources. Several **free** sources always
run with no configuration (ipapi.is, DB-IP, IPinfo demo widget, ipregistry).

Six **commercial** sources are optional and require your own API key. Without a
key, that source is simply skipped and its fields show as `null` in the report —
the check still succeeds. Configure them in `/etc/sentinel/agent.yaml` (or via
`sentinel manage` → *Edit configuration* → *Quality API keys*):

```yaml
quality:
  api_keys:
    scamalytics: ""    # https://scamalytics.com/  (fraud score)
    abuseipdb: ""      # https://www.abuseipdb.com/ (abuse confidence)
    ip2location: ""    # https://www.ip2location.io/
    ipqs: ""           # https://www.ipqualityscore.com/
    ipdata: ""         # https://ipdata.co/
    ipinfo: ""         # https://ipinfo.io/ (full API token)
```

All keys are optional and independent — set only the ones you have. After
editing, restart the agent (`systemctl restart sentinel-agent`) to apply.

## Configuration paths (FHS)

| Purpose | Path |
|---|---|
| Agent config | `/etc/sentinel/agent.yaml` |
| Master config | `/etc/sentinel/master.yaml` |
| Master DB (SQLite) | `/var/lib/sentinel/master.db` |
| Master static key (`0600`) | `/var/lib/sentinel/master_static.key` |
| GeoIP mmdb | `/var/lib/sentinel/GeoLite2-City.mmdb` |
| Cookies | `/var/lib/sentinel/cookies/` |
| Logs (cron mode) | `/var/log/sentinel/*.log` |
| systemd units | `/etc/systemd/system/sentinel-{agent,master}.service` |

## Building from source

Requires Go 1.26+.

```sh
git clone https://github.com/justinwoo280/sentinel
cd sentinel
CGO_ENABLED=0 go build -ldflags "-s -w" -o sentinel ./cmd/sentinel
```

The binary is fully static (`CGO_ENABLED=0`), ~12 MB, and cross-compiles to
`linux/amd64` and `linux/arm64`.

```sh
go test -race ./...   # run the test suite
```

## Security model

- **Transport** — EWP/v2.1: X25519 + ML-KEM-768 hybrid handshake, per-frame
  ChaCha20-Poly1305 AEAD, replay protection. Server static private key lives on
  the Master; agents carry only the public key.
- **Identity** — each agent's UUID acts as its pre-shared identity; the Master
  keeps a whitelist.
- **Command safety** — commands are a closed enum; no shell, no dynamic code, no
  message-supplied URLs. All external targets are compile-time constants. The
  message parser rejects unknown fields and over-long frames, and never panics
  on malformed input (fuzz-tested).

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
