# vox-go

Real-time voice assistant in Go. Speak, get transcribed, hear the AI respond — with interruption support and a live TUI.

## How it works

```
Mic → [VAD] → [STT] → [State Machine] → [LLM] → [TTS] → Speaker
```

1. PortAudio captures mic input (16kHz mono)
2. Silero VAD (ONNX) detects speech on every 32ms frame
3. Audio streams to STT provider via WebSocket during speech
4. When you stop speaking, transcription goes to an LLM
5. LLM response streams token-by-token to a TTS provider
6. TTS audio plays through your speakers in real-time
7. Speak during a response to interrupt it

Headphones recommended to avoid echo triggering false interruptions.

### TUI

Live terminal interface (bubbletea) showing:
- Current state (LISTENING / THINKING / RESPONDING)
- Real-time mic level and VAD probability bars
- VAD threshold marker (moves during TTS echo suppression)
- Scrolling conversation history

### State Machine

```
LISTENING ──[speech ends]──→ THINKING ──[first token]──→ RESPONDING
    ↑                                                        │
    └──────────[stream ends or user interrupts]──────────────┘
```

## Setup

### Prerequisites

```bash
brew install portaudio onnxruntime
```

### VAD Model

```bash
mkdir -p models
curl -L -o models/silero_vad.onnx \
  "https://huggingface.co/onnx-community/silero-vad/resolve/main/onnx/model.onnx"
```

### Configuration

```bash
cp .env.example .env
```

Required keys:
- `STT_API_KEY` — [Deepgram](https://console.deepgram.com/signup) (free $200 credit, no card)
- `LLM_API_KEY` — [Groq](https://console.groq.com) (free tier)

See [.env.example](.env.example) for all options including TTS model, VAD sensitivity, pipeline tuning, and log level.

## Run

```bash
go run ./cmd/vox
```

### Debug mode

```bash
LOG_LEVEL=debug go run ./cmd/vox
```

Logs go to `logs/vox_*.log`. TTS audio is saved to `logs/tts_*.wav` for inspection.

## Providers

All providers are pluggable via interfaces and swappable through env vars.

| Layer | Interface | Implementations | Config |
|---|---|---|---|
| **STT** | `Transcriber` | Deepgram (WebSocket) | `STT_PROVIDER`, `STT_MODEL` |
| **LLM** | `Streamer` | Any OpenAI-compatible API | `LLM_BASE_URL`, `LLM_MODEL` |
| **TTS** | `Synthesizer` | Deepgram (WebSocket) | `TTS_PROVIDER`, `TTS_MODEL` |

### Switching LLM providers

```bash
# Groq (default)
LLM_BASE_URL=https://api.groq.com/openai/v1
LLM_MODEL=llama-3.3-70b-versatile

# OpenAI
LLM_BASE_URL=https://api.openai.com/v1
LLM_MODEL=gpt-4o

# Ollama (local)
LLM_BASE_URL=http://localhost:11434/v1
LLM_MODEL=llama3
```

## Project Structure

```
cmd/vox/                      CLI entrypoint, component wiring
internal/
  audio/
    capture.go                Mic input (PortAudio, 16kHz)
    player.go                 Speaker output (PortAudio, 24kHz, ring buffer)
    convert.go                PCM format conversions (int16 ↔ float32/bytes)
  vad/
    vad.go                    Silero VAD via ONNX Runtime
  pipeline/
    pipeline.go               Mic → VAD → STT loop with preroll and echo suppression
  transcribe/
    transcribe.go             Transcriber interface
    deepgram.go               Deepgram STT (WebSocket streaming)
  llm/
    llm.go                    Streamer interface
    groq.go                   OpenAI-compatible streaming client
  tts/
    tts.go                    Synthesizer interface
    deepgram.go               Deepgram TTS (WebSocket streaming)
  conversation/
    conversation.go           State machine, turn-taking, interruption
    respond.go                LLM token streaming, TTS sentence buffering, playback
    text.go                   Sentence detection, markdown stripping
  tui/
    ui.go                     Bubbletea TUI (audio meters, state, conversation)
    messages.go               TUI message types
  config/
    config.go                 Env-based configuration (godotenv)
  logging/
    logging.go                Structured logging (slog) + audio dumper
docs/                         Architecture, configuration, and guides
```
