# Architecture

## System Overview

vox-go is a real-time voice assistant built as a pipeline of concurrent goroutines communicating through channels. The system follows a turn-based conversational model: the user speaks, the assistant listens, thinks, and responds — with the ability to interrupt at any point.

```
┌─────────┐     ┌─────┐     ┌─────────┐     ┌───────────────┐     ┌─────┐     ┌─────────┐
│   Mic   │────→│ VAD │────→│   STT   │────→│ State Machine │────→│ LLM │────→│   TTS   │
│PortAudio│     │Silero│    │Deepgram │     │ Conversation  │     │Groq │     │Deepgram │
│ 16kHz   │     │ ONNX │    │WebSocket│     │               │     │ SSE │     │WebSocket│
└─────────┘     └─────┘     └─────────┘     └───────────────┘     └─────┘     └─────────┘
                                                                                    │
                                                                              ┌─────────┐
                                                                              │ Speaker │
                                                                              │PortAudio│
                                                                              │  24kHz  │
                                                                              └─────────┘
```

## Goroutine Architecture

The system runs 4 persistent goroutines plus ephemeral goroutines spawned per conversation turn.

### Persistent Goroutines (lifetime of the process)

```
┌──────────────────────┐
│ Goroutine 1: Mic+VAD │──→ SpeechCh (bool)
│                      │──→ stt.SendAudio()
│ Reads 512-sample     │
│ frames at 16kHz      │
│ (32ms per frame)     │
└──────────────────────┘

┌──────────────────────┐
│ Goroutine 2: STT     │──→ TranscriptCh (string)
│                      │
│ Reads Deepgram       │
│ WebSocket results    │
└──────────────────────┘

┌──────────────────────┐
│ Goroutine 3: State   │←── SpeechCh
│ Machine (Conv.Run)   │←── TranscriptCh
│                      │←── thinkDoneCh
│ Central select loop  │
│ manages all state    │
│ transitions          │
└──────────────────────┘

┌──────────────────────┐
│ Goroutine 4:         │
│ STT Keep-Alive       │
│                      │
│ Sends heartbeat      │
│ every 5 seconds      │
└──────────────────────┘
```

### Ephemeral Goroutines (per conversation turn)

When the user finishes speaking, `think()` spawns:

```
┌──────────────────────┐
│ Think Goroutine      │  Reads LLM tokens, prints to terminal,
│                      │  buffers sentences, sends to TTS,
│                      │  calls Flush() when done,
│                      │  waits for playback, sends thinkDoneCh
└──────────────────────┘

┌──────────────────────┐
│ Playback Goroutine   │  Reads audio chunks from TTS WebSocket,
│                      │  writes to ring buffer via player.Play(),
│                      │  exits on final ErrFlushed signal
└──────────────────────┘
```

## Channel Map

```
SpeechCh (chan bool, buffer=1)
  Goroutine 1 ──→ Goroutine 3
  true = speech started, false = silence threshold reached

TranscriptCh (chan string, buffer=8)
  Goroutine 2 ──→ Goroutine 3
  Final transcript strings from STT

thinkDoneCh (chan string, buffer=1)
  Think goroutine ──→ Goroutine 3
  Full or partial assistant response text
```

## State Machine

```
                    ┌────────────────────────────────────────────┐
                    │                                            │
                    ▼                                            │
              ┌──────────┐   speech ends +    ┌──────────┐      │
              │          │   text accumulated  │          │      │
              │LISTENING │──────────────────→ │ THINKING │      │
              │          │   (500ms drain)     │          │      │
              └──────────┘                     └────┬─────┘      │
                    ▲                               │            │
                    │                          first LLM token   │
                    │                               │            │
                    │                          ┌────▼──────┐     │
                    │    stream ends            │           │     │
                    └──────────────────────────│RESPONDING │─────┘
                         or user interrupts    │           │
                                               └───────────┘
```

### LISTENING
- VAD goroutine sends audio to STT when speech detected
- STT goroutine sends final transcripts on TranscriptCh
- State machine accumulates transcript text in pendingText buffer
- On SpeechCh=false: starts 500ms drain timer to collect late transcripts
- Drain timer fires: collects remaining transcripts, calls think()

### THINKING
- Brief transitional state
- LLM request is in flight
- System prompt instructs plain text (no markdown) for TTS compatibility
- Transitions to RESPONDING on first LLM token

### RESPONDING
- Think goroutine reads LLM tokens and:
  - Prints each token to terminal
  - Buffers text until sentence boundary (. ! ? : ; \n)
  - Sends complete sentences to TTS (with markdown stripped)
- Playback goroutine reads TTS audio and writes to ring buffer
- PortAudio callback reads from ring buffer → speaker
- On interruption: cancel LLM context, clear ring buffer, back to LISTENING

## Audio Pipeline Detail

### Input (Mic → STT)

```
PortAudio mic (16kHz, mono, int16)
  │
  ├─→ Int16ToFloat32() ──→ VAD (Silero ONNX, 512 samples per inference)
  │                              │
  │                        speech detected?
  │                         yes ──→ Int16ToBytes() ──→ Deepgram STT WebSocket
  │                          no ──→ fill pre-roll buffer (last 10 frames)
  │
  │   On speech start: flush pre-roll buffer to STT first (prevents word clipping)
  │   On silence (30 frames = ~960ms): signal speech ended
```

### Output (TTS → Speaker)

```
LLM tokens
  │
  ├─→ accumulate in sentenceBuf
  │   on sentence end: stripMarkdown() ──→ Deepgram TTS WebSocket
  │                                              │
  │                                        binary audio chunks
  │                                        (linear16, 24kHz)
  │                                              │
  │                                        player.Play()
  │                                              │
  │                                   ┌─────────────────────┐
  │                                   │    Ring Buffer       │
  │                                   │  240K samples (10s)  │
  │                                   │                      │
  │                                   │  Play() writes ──→   │
  │                                   │  (blocks if full)    │
  │                                   │                      │
  │                                   │  ──→ processAudio()  │
  │                                   │  (PortAudio callback)│
  │                                   │  reads 512 samples   │
  │                                   │  per callback        │
  │                                   └─────────────────────┘
  │                                              │
  │                                         Speaker (24kHz)
```

### Backpressure

The ring buffer creates natural backpressure. When TTS sends audio faster than the speaker can play it:
1. Ring buffer fills up (10 seconds capacity)
2. `player.Play()` blocks, sleeping 10ms between retries
3. TTS receive goroutine slows down (stops calling `synth.Receive()`)
4. Deepgram TTS WebSocket buffers on its end
5. Audio plays at real-time speed, no data is lost

### Echo Suppression

While TTS is playing (`conv.Speaking = true`), the VAD goroutine:
- Still reads mic frames (keeps PortAudio buffer flowing)
- Skips all VAD detection and STT sending
- Resets speaking/silence state and clears pre-roll buffer
- This prevents the speaker output from being picked up as "user speech"

## Provider Interfaces

All external services are behind interfaces for swappability.

### Transcriber (STT)
```go
type Transcriber interface {
    Connect(ctx context.Context) error
    SendAudio(data []byte) error
    Receive() (*Result, error)
    KeepAlive() error
    Close() error
}
```

### Streamer (LLM)
```go
type Streamer interface {
    Stream(ctx context.Context, messages []Message) (<-chan string, <-chan error)
}
```
Uses OpenAI-compatible SSE streaming. Context cancellation aborts the HTTP request mid-stream (interruption mechanism).

### Synthesizer (TTS)
```go
type Synthesizer interface {
    Connect(ctx context.Context) error
    SendText(text string) error
    Flush() error
    Receive() ([]byte, error)
    Close() error
}
```
`Receive()` returns `ErrFlushed` on Deepgram's Flushed confirmation. The playback goroutine uses an atomic `finalFlushSent` flag to distinguish intermediate flushes from the final one.

## Interruption Flow

```
1. User speaks during RESPONDING state
2. VAD goroutine detects speech → SpeechCh ← true
3. State machine calls cancelResponse()
   ├─→ LLM: responseCtx cancelled → HTTP request aborted → tokenCh closes
   ├─→ TTS: think goroutine exits token loop → sends partial response on thinkDoneCh
   └─→ Player: ring buffer cleared immediately (audio stops)
4. Partial assistant response saved to conversation history
5. State → LISTENING
6. New speech is captured normally
```

## Configuration

All configuration flows through `internal/config/config.go` which loads from environment variables (with `.env` file support via godotenv).

```
.env → godotenv → os.Getenv() → Config struct → passed to constructors
```

Factory functions in `cmd/vox/main.go` (`newTranscriber`, `newSynthesizer`) select the concrete implementation based on provider config strings.
