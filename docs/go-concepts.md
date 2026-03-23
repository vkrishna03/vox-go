# Go Concepts Used in vox-go

A guide to the Go patterns and concepts used throughout this project, aimed at developers new to Go.

## Goroutines

A goroutine is a lightweight thread managed by the Go runtime. You start one with the `go` keyword:

```go
go func() {
    // runs concurrently
}()
```

vox-go uses goroutines for concurrent operations that need to happen simultaneously:
- Reading the microphone
- Sending/receiving from WebSockets
- Running the state machine
- Playing audio

Unlike OS threads, goroutines are cheap — you can run thousands. The Go scheduler multiplexes them onto a few real threads.

**Where used**: `cmd/vox/main.go` (4 persistent goroutines), `conversation.go` (2 ephemeral goroutines per turn)

## Channels

Channels are typed pipes for communication between goroutines:

```go
ch := make(chan string, 8)  // buffered channel (holds up to 8 values)
ch <- "hello"               // send (blocks if buffer full)
msg := <-ch                 // receive (blocks if buffer empty)
```

vox-go uses channels as the "nervous system" connecting goroutines:
- `SpeechCh (chan bool)` — VAD goroutine tells the state machine when speech starts/stops
- `TranscriptCh (chan string)` — STT goroutine sends transcriptions to the state machine
- `thinkDoneCh (chan string)` — think goroutine signals completion to the state machine

**Key insight**: channels make goroutines communicate by passing messages instead of sharing memory. No locks needed.

## Select

`select` waits on multiple channels simultaneously:

```go
select {
case msg := <-ch1:
    // ch1 fired first
case <-ch2:
    // ch2 fired first
case <-ctx.Done():
    // context was cancelled
}
```

It's like a `switch` statement but for concurrent events. Whichever channel has data first, that case runs.

**Where used**: The state machine in `conversation.go` uses `select` to listen to SpeechCh, TranscriptCh, thinkDoneCh, and timer channels all at once.

## Context

A `context.Context` carries cancellation signals across goroutines:

```go
ctx, cancel := context.WithCancel(parentCtx)
// pass ctx to goroutines...
cancel()  // all goroutines checking ctx.Done() will stop
```

vox-go uses contexts for:
- **Ctrl+C handling**: `signal.NotifyContext` creates a context that cancels on SIGINT
- **Interruption**: each LLM response gets a child context. When the user speaks during a response, `cancelResponse()` cancels it, which aborts the HTTP request mid-stream.

**Where used**: `cmd/vox/main.go` (signal context), `conversation.go` (response context)

## Interfaces

An interface defines a set of methods without implementation:

```go
type Streamer interface {
    Stream(ctx context.Context, messages []Message) (<-chan string, <-chan error)
}
```

Any type that has those methods satisfies the interface — no explicit declaration needed. This is "duck typing" at compile time.

vox-go uses interfaces for all external services:
- `Transcriber` — any STT provider
- `Streamer` — any LLM provider
- `Synthesizer` — any TTS provider

**Why it matters**: the conversation layer doesn't know about Deepgram or Groq. It only knows the interface. Swapping providers means implementing the interface and changing a config value.

## WaitGroup

A `sync.WaitGroup` waits for a set of goroutines to finish:

```go
var wg sync.WaitGroup

wg.Add(1)           // +1 goroutine
go func() {
    defer wg.Done() // -1 when done
}()

wg.Wait()           // blocks until counter == 0
```

**Where used**: `cmd/vox/main.go` waits for all goroutines on shutdown. `conversation.go` waits for the playback goroutine to finish.

## Defer

`defer` schedules a function to run when the current function returns:

```go
func run() error {
    rec, _ := audio.NewRecorder()
    defer rec.Close()  // runs when run() returns, no matter how

    // ... use rec ...
    return nil  // rec.Close() runs here
}
```

Multiple defers run in reverse order (LIFO). Used for cleanup — closing connections, stopping streams, releasing resources.

## Atomic Operations

`sync/atomic` provides lock-free operations on shared variables:

```go
var flag atomic.Bool
flag.Store(true)     // set
if flag.Load() {     // read
    // ...
}
```

Safer and faster than mutexes for simple flags. vox-go uses `atomic.Bool` for the `finalFlushSent` flag that coordinates between the think goroutine and the playback goroutine.

## Mutex

`sync.Mutex` protects shared data from concurrent access:

```go
var mu sync.Mutex
mu.Lock()
// only one goroutine can be here at a time
mu.Unlock()
```

**Where used**: The ring buffer in `player.go`. The PortAudio callback (running on an audio thread) and `Play()` (running on a goroutine) both access the same buffer, so a mutex prevents corruption.

## Error Handling

Go doesn't have exceptions. Functions return errors as values:

```go
result, err := doSomething()
if err != nil {
    return fmt.Errorf("context: %w", err)  // wrap with context
}
```

`%w` wraps the original error so callers can unwrap it later. This builds an error chain: `"recorder: open stream: device not found"`.

## Struct Methods (Receivers)

Methods are attached to types via receivers:

```go
type Player struct { ... }

func (p *Player) Play(data []byte) error {
    // p is like 'this' or 'self'
}
```

`*Player` (pointer receiver) means the method can modify the struct. `Player` (value receiver) would get a copy.

## Slices

Slices are Go's dynamic arrays:

```go
buf := make([]int16, 512)     // allocate 512 elements
buf = append(buf, sample)     // grow
copy(dst, src)                // copy elements
sub := buf[10:20]             // slice of elements 10-19 (no copy, shares memory)
```

**Where used**: audio buffers throughout. The pre-roll buffer uses slice operations to implement a ring buffer: `preroll = preroll[1:]` drops the oldest frame.

## Struct Tags

Tags attach metadata to struct fields:

```go
type Message struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}
```

`json:"role"` tells `encoding/json` to use "role" as the JSON key instead of "Role". Used for all API request/response types.
