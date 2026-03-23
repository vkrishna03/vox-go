# vox-go

Real-time voice assistant in Go. Captures mic audio, detects speech with Silero VAD, transcribes with streaming STT, and responds via LLM — all with interruption support.

## Architecture

```
Mic → [VAD] → [STT Provider] → [State Machine] → [LLM Provider]
                                      ↕
                                  [Terminal]
```

### State Machine

```
LISTENING ──[speech ends]──→ THINKING ──[first token]──→ RESPONDING
    ↑                                                        │
    └──────────[stream ends or user interrupts]──────────────┘
```

### Pipeline

| Component | Description |
|---|---|
| **Audio Capture** | PortAudio, 16kHz mono, 512-sample frames (32ms) |
| **VAD** | Silero VAD v6 via ONNX Runtime, runs on every frame |
| **STT** | Pluggable interface — currently Deepgram (WebSocket streaming) |
| **LLM** | Pluggable interface — any OpenAI-compatible API (Groq, OpenAI, Ollama) |
| **Conversation** | State machine with turn-taking and mid-response interruption |

## Setup

### Prerequisites

```bash
brew install portaudio onnxruntime
```

### Model

Download Silero VAD ONNX model:

```bash
mkdir -p models
curl -L -o models/silero_vad.onnx \
  "https://huggingface.co/onnx-community/silero-vad/resolve/main/onnx/model.onnx"
```

### Configuration

```bash
cp .env.example .env
# edit .env with your API keys
```

See [.env.example](.env.example) for all options.

## Run

```bash
go run ./cmd/vox
```

Speak into your mic. Transcriptions appear in real-time. The LLM responds after you stop speaking. Speak again during a response to interrupt it.

## Project Structure

```
cmd/vox/                    CLI entrypoint
internal/
  audio/capture.go          Mic capture via PortAudio
  vad/vad.go                Silero VAD via ONNX Runtime
  transcribe/
    transcribe.go           Transcriber interface
    deepgram.go             Deepgram WebSocket implementation
  llm/
    llm.go                  Streamer interface
    groq.go                 OpenAI-compatible streaming client
  conversation/
    conversation.go         State machine orchestrator
  config/config.go          Env-based configuration
  server/server.go          HTTP server (unused, for future use)
```
