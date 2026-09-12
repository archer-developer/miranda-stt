# miranda-stt — project notes for Claude Code

Wyoming STT microservice for Home Assistant. Accepts TCP connections on the
Wyoming protocol, streams PCM audio to the Google Gemini Multimodal Live API
(BidiGenerateContent WebSocket), and returns the transcript. No HTTP, no MCP —
this is a pure TCP server, which makes it different from the rest of the
Miranda family built on the MCP skeleton.

## Conventions

Same as `miranda-service-skeleton/CLAUDE.md` (explanatory comments, no CGO,
no Docker, secrets via env vars, error wrapping with `fmt.Errorf`, no DI
framework). Deviations specific to this service:

- **No HTTP server** — the service exposes only a Wyoming TCP server on port
  10300. There is no `/healthz`, no MCP endpoint, no bearer token. Liveness
  is checked via `systemctl --user status miranda-stt`.
- **No config/*.yaml by default** — the service runs entirely off built-in
  defaults plus a `.env` file for `GEMINI_API_KEY`. A `config/config.yaml`
  is only needed if you want to change defaults (port, model, languages, etc.).
- **One dependency beyond stdlib**: `github.com/coder/websocket` for the
  Gemini WebSocket. Added because Go stdlib has no WebSocket; coder/websocket
  was chosen over gorilla/websocket (unmaintained) and nhooyr.io/websocket
  (same library, renamed). No other third-party dependencies.

## Architecture

```
Home Assistant (satellite / voice PE)
    |  TCP :10300 (Wyoming protocol)
    v
wyoming.Server  (internal/wyoming/server.go)
    → wyoming.Session per connection (internal/wyoming/session.go)
         |  reads Wyoming headers + PCM payload
         |  writes "info" and "transcript" responses
         |
         v
    gemini.Client  (internal/gemini/client.go)
         |  WebSocket wss://generativelanguage.googleapis.com/...
         |  setup → realtime_input stream → server_content deltas
         v
    Google Gemini Multimodal Live API
```

### Session lifecycle

1. Client sends `{"type":"describe"}` — server replies with `{"type":"info", ...}`.
2. Client sends `{"type":"transcribe"}` — server waits.
3. Client sends `{"type":"audio-start"}` — server dials Gemini, performs setup handshake.
4. Client sends repeated `{"type":"audio-chunk", "payload_length":N}\n<N bytes PCM>`.
   - A goroutine reads chunks and forwards them to Gemini via `realtime_input`.
   - A second goroutine reads Gemini events, accumulating text deltas.
5. Two possible end triggers (dual trigger, first wins):
   - **Trigger A (HA)**: `{"type":"audio-stop"}` — server calls `SignalEndOfInput`
     on Gemini, waits up to `gemini_turn_complete_timeout_ms` for the final delta.
   - **Trigger B (Gemini)**: `serverContent.turnComplete=true` arrives.
6. Server sends `{"type":"transcript", "data":{"text":"..."}}`.
7. Loop back to step 1 (the connection stays open for the next utterance).

### Audio dump (WAV)

When `audio_dump_dir` is set in config, each session writes accumulated PCM
to a valid RIFF WAV file:
`{dir}/{YYYYMMDD_HHMMSS}_{session_id}_{trigger_reason}.wav`

Purpose: building an acoustic dataset from Voice Preview Edition microphones
for fine-tuning openWakeWord / microWakeWord.

## Testing

```bash
make check   # fmt + lint + go test ./... -race
```

No automated integration test against the live Gemini API — that would require
a real API key and incur billing. Unit-testable parts:
- `internal/wyoming/protocol.go` — read/write roundtrip
- `internal/audio/wav_writer.go` — header bytes, file written correctly
- `internal/config/config.go` — validate() rejects bad fields

## Deploying

```bash
MIRANDA_DEPLOY_HOST=user@host ./scripts/deploy.sh
```

First-time setup on the host:
1. `mkdir -p ~/miranda-stt/config`
2. Copy `.env.example` to `~/miranda-stt/.env`, set `GEMINI_API_KEY=<key>`
3. Optionally copy `config/config.yaml.dist` to `~/miranda-stt/config/config.yaml` and edit

Connecting to Home Assistant:
- Settings → Devices & Services → Add Integration → Wyoming Protocol
- Host: `<server_ip>`, Port: `10300`
- The STT provider appears as "gemini-live-stt" in Voice Assistant settings.

## What's deliberately not here

- **Docker / Dockerfile** — host-native `systemd --user`, same as all Miranda services.
- **HTTP / MCP / bearer token** — this service speaks Wyoming (TCP), not MCP.
- **VAD on the server side** — Home Assistant's satellite handles VAD; Gemini's
  built-in VAD is the secondary trigger. No third-party VAD library needed.
- **Speaker identification** — handled upstream by the wake-word layer in HA /
  ESPHome firmware, not in this STT microservice.
