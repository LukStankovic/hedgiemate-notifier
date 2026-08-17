# HedgieMate Notifier

A lightweight Go sidecar that monitors TeslaMate MQTT topics and sends push notification events to the HedgieMate relay server. Designed to run alongside your TeslaMate Docker stack.

## How It Works

1. Connects to your TeslaMate MQTT broker
2. Monitors vehicle state topics (driving, charging, battery, geofence, etc.)
3. Detects state transitions (e.g., charging started, drive ended)
4. Sends events to the HedgieMate relay server, which delivers push notifications to your iOS device

## Prerequisites

- A running TeslaMate instance with MQTT broker (Mosquitto)
- A HedgieMate user token (generated in the HedgieMate iOS app under Settings > Push Notifications)

## Quick Start

### Docker Compose (recommended)

Add the notifier to your existing TeslaMate `docker-compose.yml`:

```yaml
services:
  hedgiemate-notifier:
    image: hedgiemate/notifier:latest
    restart: unless-stopped
    depends_on:
      - mosquitto
    environment:
      HEDGIEMATE_USER_TOKEN: "hm_your_token_here"
      MQTT_HOST: "mosquitto"
      MQTT_PORT: "1883"
      CAR_IDS: "1"
      LOG_LEVEL: "info"
      SERVER_ID: "your-server-uuid"
    volumes:
      - hedgiemate-notifier-data:/data

volumes:
  hedgiemate-notifier-data:
```

The volume is optional. It caches car names so they survive a `docker compose
down`; without it the relay fills the name in anyway. If you do mount one, use
a named volume rather than a bind mount — the container runs as a non-root user
and cannot write to a host directory Docker created as root.

Then run:

```bash
docker compose up -d hedgiemate-notifier
```

### Docker Run

```bash
docker run -d \
  --name hedgiemate-notifier \
  --restart unless-stopped \
  -e HEDGIEMATE_USER_TOKEN="hm_your_token_here" \
  -e MQTT_HOST="your-mqtt-host" \
  -e MQTT_PORT="1883" \
  -e CAR_IDS="1" \
  hedgiemate/notifier:latest
```

### Build from Source

```bash
go build -o hedgiemate-notifier .
HEDGIEMATE_USER_TOKEN=hm_xxx MQTT_HOST=localhost ./hedgiemate-notifier
```

## Configuration

All configuration is via environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `HEDGIEMATE_USER_TOKEN` | Yes | - | Your linking token from the HedgieMate iOS app (`hm_xxx`) |
| `MQTT_HOST` | Yes | - | MQTT broker hostname |
| `MQTT_PORT` | No | `1883` | MQTT broker port |
| `MQTT_USERNAME` | No | - | MQTT authentication username |
| `MQTT_PASSWORD` | No | - | MQTT authentication password |
| `MQTT_CLIENT_ID` | No | `hedgiemate-notifier` | MQTT client ID |
| `MQTT_TLS` | No | `false` | Set to `true` for TLS. Switches the default port to `8883` |
| `MQTT_NAMESPACE` | No | - | TeslaMate `MQTT_NAMESPACE`, if you set one. Topics become `teslamate<namespace>/cars/#` |
| `CAR_IDS` | No | `1` | Comma-separated car IDs to monitor |
| `RELAY_URL` | No | `https://push.hedgiemate.com` | Relay server URL |
| `SERVER_ID` | Yes | - | Server UUID from the iOS app, included in the snippet it gives you. The notifier starts without it, but per-server and per-car notification settings made in the app are then ignored |
| `BATTERY_LOW_THRESHOLD` | No | `20` | Battery low notification threshold (%) |
| `BATTERY_HIGH_THRESHOLD` | No | `90` | Battery full notification threshold (%) |
| `DISTANCE_UNIT` | No | `km` | `km` or `mi` |
| `LOG_LEVEL` | No | `info` | Log level: `debug`, `info`, `warn`, `error` |

## Supported Events

| Event | Trigger |
|-------|---------|
| `drive_started` | Vehicle state transitions to "driving" |
| `drive_ended` | Vehicle state transitions from "driving" |
| `charging_started` | Charging state transitions to "Charging" |
| `charging_completed` | Charging state transitions to "Complete" |
| `charging_interrupted` | Charging stops unexpectedly (not "Complete") |
| `battery_low` | Battery drops below threshold (once per session) |
| `battery_full` | Battery rises above threshold (once per session) |
| `charger_connected` | Charger plugged in |
| `charger_disconnected` | Charger unplugged |
| `software_update` | New software update available |
| `geofence_entered` | Vehicle enters a geofence |
| `geofence_exited` | Vehicle exits a geofence |
| `vehicle_falling_asleep` | Vehicle is going to sleep |
| `vehicle_asleep` | Vehicle goes to sleep |
| `vehicle_woke` | Vehicle wakes up |
| `sentry_recording` | Sentry Mode starts recording (center_display_state == 7) |
| `live_activity_update` | Periodic charging data for Live Activities (30s/60s) |
| `live_activity_driving_update` | Periodic drive data for Live Activities (15s/30s) |

## Multiple Cars

Monitor multiple vehicles by setting `CAR_IDS` to a comma-separated list:

```
CAR_IDS=1,2,3
```

Each car has its own independent state machine.

## Architecture

```
MQTT Broker ──> MQTT Client ──> State Machine ──> Event Emitter ──> Relay Server ──> APNs ──> iOS
                                (per car)         (5s debounce)     (HMAC signed)
```

- **MQTT Client**: Subscribes to TeslaMate topics, auto-reconnects on connection loss
- **State Machine**: Tracks per-car state, detects transitions, manages battery threshold flags
- **Event Emitter**: 5-second debounce window, prevents duplicate events
- **Relay Client**: HMAC-SHA256 signed requests, 3 retries with exponential backoff

## Changelog

See the [GitHub Releases](https://github.com/LukStankovic/hedgiemate-notifier/releases) page for release notes, or [`CHANGELOG.md`](https://github.com/LukStankovic/hedgiemate-notifier/blob/master/CHANGELOG.md) in the repo.

## License

MIT — see [`LICENSE`](LICENSE).

---

**Disclaimer:** This project is an unofficial community tool and is not affiliated with, endorsed by, or supported by the official TeslaMate project.
