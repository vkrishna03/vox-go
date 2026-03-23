# Python vs Go for Vox

Why this project is in Go instead of Python, with real numbers.

## The pipeline

```
Mic → VAD (every 32ms) → STT (WebSocket) → LLM (HTTP stream) → TTS (WebSocket) → Speaker
```

Every 32ms, a frame goes through VAD. When speech ends, the transcript hits an LLM, streams back through TTS, and plays through the speaker. The question is: where does each language spend its time?

## Where time actually goes

A typical turn (user speaks → hears response) breaks down like this:

| Stage | Time | Bottleneck |
|---|---|---|
| Silence detection | 960ms | Fixed (30 frames × 32ms) |
| Drain timer | 500ms | Fixed (wait for trailing words) |
| STT final transcript | ~50-100ms | Network (Deepgram) |
| LLM first token | ~200-400ms | Network (Groq/OpenAI) |
| TTS first audio chunk | ~100-200ms | Network (Deepgram) |
| Local processing | ~1-5ms | CPU |
| **Total** | **~1.8-2.1s** | |

**~97% of latency is network + fixed timers.** The local CPU work (VAD, audio conversion, state machine) is a rounding error in both languages.

So Go vs Python doesn't change the number the user feels. Both land around 1.8-2.1s from silence to first audio.

## Then why Go?

The latency that matters isn't the average — it's the worst case. Real-time audio has a hard deadline: every 21ms (512 samples at 24kHz), the speaker callback must deliver samples or you hear a pop/glitch.

### GIL + GC = audio glitches

Python's GIL means only one thread runs at a time. The PortAudio speaker callback runs in a C thread and needs the GIL to touch Python objects. If the main thread is doing VAD inference or LLM token processing, the callback waits.

CPython's GC is stop-the-world. A collection during audio playback pauses everything for 10-50ms. That's 1-2 missed speaker callbacks = an audible pop.

Go's GC is concurrent and sub-millisecond. The speaker callback (`player.go:processAudio`) grabs a mutex, copies samples from a ring buffer, and returns. No GIL, no stop-the-world.

| Scenario | Go | Python |
|---|---|---|
| Normal playback | Smooth | Smooth |
| Long LLM response (30s+) | Smooth | Occasional pops (GC) |
| Rapid interruptions | Clean cut | 50-100ms stale audio (GIL contention) |
| VAD running during playback | No interference | Blocks event loop or fights GIL |

### Concurrency model

Vox runs 4 concurrent paths:

1. Mic → VAD → STT (CPU + I/O)
2. STT → transcript channel (I/O)
3. Conversation state machine (CPU + I/O)
4. TTS → speaker playback (I/O + real-time audio)

In Go, these are goroutines communicating via channels. Path 1 is CPU-bound (VAD inference) and runs on its own OS thread automatically. No special handling.

In Python with asyncio:
- Paths 2, 3, 4 work fine (I/O-bound)
- Path 1 blocks the event loop (CPU-bound VAD). You need `run_in_executor` to push it to a thread pool, adding ~0.1-0.5ms dispatch overhead per frame and GIL contention
- The interrupt pattern (cancel response + clear audio buffer) needs careful coordination between asyncio tasks and threads

It works, but it's more fragile.

### Resource usage

| Metric | Go | Python (ONNX) | Python (PyTorch) |
|---|---|---|---|
| Memory at idle | ~20 MB | ~100 MB | ~500 MB |
| Binary / install size | ~15 MB | ~50 MB | ~2 GB |
| Startup time | ~50ms | ~300ms | ~1.5s |
| Dependencies | 4 Go modules | ~15 pip packages | ~30 pip packages |
| Distribution | Single binary | venv + native deps | venv + native deps + CUDA? |

### Per-frame overhead (every 32ms)

| Operation | Go | Python |
|---|---|---|
| int16 → float32 (512 samples) | ~0.002ms | ~0.05ms (numpy) |
| VAD inference (ONNX) | ~0.3-0.5ms | ~0.3-0.5ms |
| int16 → bytes (for STT) | ~0.002ms | ~0.01ms (numpy) |
| Channel/queue dispatch | ~0.001ms | ~0.1ms (asyncio) |
| **Frame budget used** | **~1-2%** | **~2-3%** |

Both fit comfortably in 32ms. The difference is headroom — Go uses ~1% of the frame budget, leaving 99% for jitter, GC, and system load. Python uses ~2-3%, still fine, but GC pauses can spike to 50ms (150% of frame budget).

## Where Python would win

- **Provider SDKs**: Deepgram, OpenAI, Groq all have official Python SDKs with streaming built in. The Go code hand-rolls WebSocket protocols (~200 lines that would be ~20 in Python).
- **VAD setup**: `pip install silero-vad` vs downloading ONNX models and linking `libonnxruntime.dylib`.
- **Prototyping speed**: The state machine + goroutine wiring in `main.go` is ~250 lines. Python asyncio equivalent would be ~120 lines.
- **ML ecosystem**: If we add wake word detection, speaker diarization, or local whisper — Python has these as pip packages. Go needs CGO bindings or subprocess calls.

## What if STT and TTS are local?

The analysis above assumes cloud providers (Deepgram, Groq). Network latency dominates, so Go vs Python barely matters for perceived speed. But if you run STT and TTS locally, the entire picture flips — network is gone, and CPU becomes the bottleneck.

### The new pipeline

```
Mic → VAD → Local STT (Whisper) → Local LLM (Ollama) → Local TTS (Piper/Kokoro) → Speaker
```

### New latency breakdown

| Stage | Cloud | Local | What changed |
|---|---|---|---|
| Silence detection | 960ms | 960ms | Same |
| Drain timer | 500ms | 500ms | Same |
| STT transcription | ~50-100ms (network) | ~300-1500ms (Whisper, CPU-dependent) | **10-15x slower**, now CPU-bound |
| LLM first token | ~200-400ms (network) | ~500-2000ms (Ollama, model-dependent) | **2-5x slower**, now CPU/GPU-bound |
| TTS first audio | ~100-200ms (network) | ~50-200ms (Piper/Kokoro are fast) | Similar or faster |
| **Total** | **~1.8-2.1s** | **~2.3-5.2s** | |

Network latency is gone, but it's replaced by local compute that's **much heavier**. The bottleneck shifts from "waiting for packets" to "waiting for inference."

### This is where Go vs Python actually matters

With cloud providers, local CPU work was ~3% of total latency. With local providers, it's **60-80%**. Now the language runtime's overhead stacks on top of already-heavy inference.

#### CPU contention: the real problem

With local providers, your CPU is simultaneously running:

| Task | CPU load | Frequency |
|---|---|---|
| VAD inference (Silero ONNX) | Light (~0.5ms) | Every 32ms, continuous |
| STT inference (Whisper) | **Heavy** (~300-1500ms) | After each utterance |
| LLM inference (Ollama/llama.cpp) | **Heavy** (saturates cores) | After each transcript |
| TTS inference (Piper) | Moderate (~50-200ms) | Per sentence |
| Audio playback callback | Tiny but **hard real-time** | Every 21ms, cannot miss |

In Go, these run on separate goroutines across OS threads. The Go scheduler multiplexes them, and the lightweight speaker callback never starves because it's just a mutex + memcpy from a ring buffer.

In Python, everything fights the GIL:

- Whisper inference holds the GIL during Python-level processing (between CUDA/MKL calls)
- The speaker callback can't run during those gaps
- LLM token processing (even just receiving from Ollama) takes GIL time
- You end up needing multiprocessing (not just threading) to truly parallelize — but then you're passing audio data between processes via pipes/shared memory, adding latency and complexity

#### Concrete impact

| Scenario | Go | Python |
|---|---|---|
| VAD during Whisper inference | Both run on separate threads, no conflict | GIL: VAD waits for Whisper's Python frames. Possible 5-10ms VAD delay → missed speech onset |
| Speaker callback during LLM streaming | Ring buffer read runs freely | GIL contention → audio glitches every few seconds under load |
| Whisper + LLM + TTS overlapping | Goroutines on separate cores, full parallelism | GIL serializes Python frames. Effective single-core for Python-level work |
| Interruption during TTS inference | Cancel context, clear buffer: ~0.1ms | TTS inference may not check for cancellation mid-batch. 50-500ms delay before audio stops |

#### The frame budget gets tight

With cloud providers, the 32ms VAD frame had 99% headroom. With local inference saturating CPU:

| Metric | Go | Python |
|---|---|---|
| VAD frame budget used (idle) | ~1-2% | ~2-3% |
| VAD frame budget used (during Whisper) | ~2-3% (separate core) | ~15-40% (GIL contention, core sharing) |
| Speaker callback jitter (during LLM) | <1ms | 5-50ms (GC + GIL) |
| Risk of audio dropout | Negligible | Real, especially on 4-core machines |

### Python's advantage gets stronger too

Here's the twist: local providers are **Python-native**. The ecosystem tilts hard toward Python:

| Local provider | Python | Go |
|---|---|---|
| Whisper (STT) | `import whisper` — native, GPU-accelerated | CGO bindings to whisper.cpp, or subprocess to `whisper-server` |
| Piper (TTS) | `pip install piper-tts` | Subprocess call or HTTP to a Piper server |
| Kokoro (TTS) | `pip install kokoro` | No Go bindings exist |
| Ollama (LLM) | HTTP API (same in both) | HTTP API (same in both) |
| Silero VAD | `pip install silero-vad` (native PyTorch) | ONNX Runtime via CGO (current approach) |

In Go, "local Whisper" likely means running a whisper.cpp HTTP server as a sidecar process and hitting it over localhost — adding ~5-10ms of HTTP overhead per request, but avoiding GIL issues entirely.

In Python, you get direct in-process access to Whisper's GPU tensors, streaming partial results, and tighter integration — but you pay the GIL/GC tax.

### The local tradeoff, in one table

| Concern | Go (with local providers) | Python (with local providers) |
|---|---|---|
| Total turn latency | ~2.3-5.0s | ~2.3-5.2s (similar, inference-dominated) |
| Audio reliability | Solid — no GIL, concurrent GC | Risky — GIL contention under heavy CPU load |
| Integration effort | High — subprocess/HTTP wrappers for Whisper, Piper | Low — pip install, direct function calls |
| GPU utilization | Via sidecar processes (each manages own GPU) | Direct — PyTorch shares GPU context in-process |
| Memory | ~20MB + sidecar processes (~500MB each) | ~1-2GB (all models in one process) |
| Architecture | Microservice-ish (Go orchestrator + Python/C++ sidecars) | Monolith (everything in one Python process) |

### Bottom line for local

With cloud providers: Go wins clearly. Network dominates latency, and Go's runtime gives you free reliability.

With local providers: it's a real tradeoff.
- **Go** keeps audio reliable but you're duct-taping Python/C++ inference engines via HTTP sidecars. You become an orchestrator, not a pipeline.
- **Python** gives native access to the entire ML stack but you're fighting the GIL to keep audio smooth. You need careful architecture (multiprocessing, larger audio buffers) to avoid glitches.

If local inference is the goal, a hybrid makes sense: Python for the inference-heavy work (STT, TTS), Go for the real-time audio pipeline and orchestration, communicating over gRPC or Unix sockets. You get the best of both — but at the cost of two codebases.

## Summary

**Cloud providers (current)**:
- Go vs Python barely matters for perceived latency (~97% is network)
- Go wins on reliability (no GIL/GC audio glitches) and simplicity (goroutines + channels)
- Python wins on developer velocity and SDK availability

**Local providers**:
- Language choice matters much more (~60-80% of latency is local CPU)
- Go keeps audio clean but needs sidecar processes for ML inference
- Python has native ML access but GIL makes real-time audio fragile under load
- Hybrid architecture (Go audio + Python inference) is likely the best of both worlds
