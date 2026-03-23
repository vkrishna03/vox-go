# Adding Providers

All external services (STT, LLM, TTS) are behind interfaces. Adding a new provider means implementing the interface, registering it in the factory, and adding env vars.

## Adding an STT Provider

### 1. Implement the interface

Create `internal/transcribe/assemblyai.go` (or whatever provider):

```go
package transcribe

import "context"

type AssemblyAI struct {
    apiKey string
    // connection fields...
}

func NewAssemblyAI(apiKey string) *AssemblyAI {
    return &AssemblyAI{apiKey: apiKey}
}

func (a *AssemblyAI) Connect(ctx context.Context) error {
    // establish WebSocket/connection
}

func (a *AssemblyAI) SendAudio(data []byte) error {
    // send raw PCM audio bytes
    // format: linear16, 16kHz, mono (matches audio.SampleRate)
}

func (a *AssemblyAI) Receive() (*Result, error) {
    // read next transcription result
    // return &Result{Text: "...", IsFinal: true/false}
    // parse provider-specific JSON into the generic Result struct
}

func (a *AssemblyAI) KeepAlive() error {
    // send heartbeat if provider requires it
    // return nil if not needed
}

func (a *AssemblyAI) Close() error {
    // graceful shutdown
}
```

The interface (`internal/transcribe/transcribe.go`):

```go
type Transcriber interface {
    Connect(ctx context.Context) error
    SendAudio(data []byte) error
    Receive() (*Result, error)
    KeepAlive() error
    Close() error
}

type Result struct {
    Text    string
    IsFinal bool
}
```

### 2. Register in the factory

In `cmd/vox/main.go`, add a case to `newTranscriber()`:

```go
func newTranscriber(cfg *config.Config) (transcribe.Transcriber, error) {
    switch cfg.STTProvider {
    case "deepgram":
        return transcribe.NewDeepgram(cfg.STTAPIKey, cfg.STTModel), nil
    case "assemblyai":
        return transcribe.NewAssemblyAI(cfg.STTAPIKey), nil
    default:
        return nil, fmt.Errorf("unknown STT provider: %s", cfg.STTProvider)
    }
}
```

### 3. Add config

In `internal/config/config.go`, add any provider-specific fields if needed. The user switches providers via:

```bash
STT_PROVIDER=assemblyai
STT_API_KEY=your_key
```

### Audio format contract

STT providers receive audio as:
- Raw PCM bytes, little-endian int16 (linear16)
- 16kHz sample rate, mono
- Sent in 512-sample frames (~32ms each)

The provider must return `*Result` with:
- `Text`: the transcribed text (empty string for non-result messages)
- `IsFinal`: true for confirmed transcriptions, false for interim guesses

---

## Adding an LLM Provider

### 1. Implement the interface

Create `internal/llm/anthropic.go`:

```go
package llm

import "context"

type AnthropicClient struct {
    apiKey string
}

func NewAnthropicClient(apiKey string) *AnthropicClient {
    return &AnthropicClient{apiKey: apiKey}
}

func (c *AnthropicClient) Stream(ctx context.Context, messages []Message) (<-chan string, <-chan error) {
    tokenCh := make(chan string, 16)
    errCh := make(chan error, 1)

    go func() {
        defer close(tokenCh)
        defer close(errCh)

        // Make HTTP request with ctx (for cancellation/interruption)
        // Parse streaming response
        // Send each token on tokenCh
        // Send final error (or nil) on errCh
    }()

    return tokenCh, errCh
}
```

The interface (`internal/llm/llm.go`):

```go
type Streamer interface {
    Stream(ctx context.Context, messages []Message) (<-chan string, <-chan error)
}

type Message struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}
```

### Key requirements

- `Stream()` must respect `ctx` cancellation — this is how interruption works. When the user speaks during a response, the context is cancelled, which must abort the HTTP request.
- `tokenCh` is buffered at 16. Send each text token as it arrives.
- `errCh` must receive exactly one value (nil on success, error on failure) then close.
- `context.Canceled` errors are expected during interruption — the conversation layer handles this.

### 2. Register in main.go

Currently the LLM doesn't use a factory because `OpenAIClient` works with any OpenAI-compatible API via `LLM_BASE_URL`. If your provider isn't OpenAI-compatible, add a factory similar to STT/TTS.

### Using OpenAI-compatible providers (no code changes)

Many providers expose OpenAI-compatible endpoints:

```bash
# Groq
LLM_BASE_URL=https://api.groq.com/openai/v1

# OpenAI
LLM_BASE_URL=https://api.openai.com/v1

# Ollama
LLM_BASE_URL=http://localhost:11434/v1

# Together AI
LLM_BASE_URL=https://api.together.xyz/v1
```

---

## Adding a TTS Provider

### 1. Implement the interface

Create `internal/tts/elevenlabs.go`:

```go
package tts

import "context"

type ElevenLabs struct {
    apiKey string
}

func NewElevenLabs(apiKey string) *ElevenLabs {
    return &ElevenLabs{apiKey: apiKey}
}

func (e *ElevenLabs) Connect(ctx context.Context) error {
    // establish WebSocket/connection
}

func (e *ElevenLabs) SendText(text string) error {
    // send text for synthesis
    // called once per sentence (not per token)
}

func (e *ElevenLabs) Flush() error {
    // signal that no more text is coming
    // provider should finish generating remaining audio
}

func (e *ElevenLabs) Receive() ([]byte, error) {
    // read next audio chunk
    // return raw PCM bytes (linear16, 24kHz, mono)
    // return nil, tts.ErrFlushed when all audio after Flush() is delivered
}

func (e *ElevenLabs) Close() error {
    // graceful shutdown
}
```

The interface (`internal/tts/tts.go`):

```go
type Synthesizer interface {
    Connect(ctx context.Context) error
    SendText(text string) error
    Flush() error
    Receive() ([]byte, error)
    Close() error
}
```

### Key requirements

- `Receive()` must return raw PCM audio: little-endian int16, 24kHz, mono. If the provider returns a different format, convert before returning.
- `Receive()` must return `tts.ErrFlushed` after delivering all audio following a `Flush()` call. The conversation layer uses this to know when playback is complete.
- Text is sent in complete sentences (not individual tokens). The conversation layer handles sentence buffering.
- Markdown is stripped before sending. Text arrives as plain spoken language.

### ErrFlushed behavior

The conversation layer tracks a `finalFlushSent` atomic bool:
- Intermediate `ErrFlushed` (from provider auto-flushing) → ignored, continue receiving
- `ErrFlushed` after `finalFlushSent` is true → playback goroutine exits

Your provider must return `ErrFlushed` when it confirms all buffered audio has been sent after a `Flush()` call.

### 2. Register in the factory

In `cmd/vox/main.go`:

```go
func newSynthesizer(cfg *config.Config) (tts.Synthesizer, error) {
    switch cfg.TTSProvider {
    case "deepgram":
        return tts.NewDeepgramTTS(cfg.TTSAPIKey, cfg.TTSModel), nil
    case "elevenlabs":
        return tts.NewElevenLabs(cfg.TTSAPIKey), nil
    default:
        return nil, fmt.Errorf("unknown TTS provider: %s", cfg.TTSProvider)
    }
}
```

### 3. Add config

```bash
TTS_PROVIDER=elevenlabs
TTS_API_KEY=your_key
TTS_MODEL=voice_id_here
```
