# Troubleshooting

## Setup Issues

### `Package portaudio-2.0 was not found`

PortAudio isn't installed:
```bash
brew install portaudio
```

### `Error loading ONNX shared library`

ONNX Runtime isn't installed or has a version mismatch:
```bash
brew reinstall onnxruntime
```

If you see errors about `libabsl` or `libre2`:
```bash
brew reinstall re2 onnxruntime
```

### `DEEPGRAM_API_KEY env var is required` (or STT/LLM)

Missing `.env` file or keys not set:
```bash
cp .env.example .env
# edit .env with your actual keys
```

Make sure you're running from the project root directory — godotenv loads `.env` relative to the working directory.

### `.env` not loading

Run from the project root:
```bash
cd /path/to/vox-go
go run ./cmd/vox
```

If still not working, you'll see: `warning: no .env file found, using environment variables`. Export variables manually or fix the path.

## Audio Issues

### No microphone input / VAD never triggers

1. Check macOS microphone permissions: System Settings → Privacy & Security → Microphone
2. Try lowering the VAD threshold: `VAD_THRESHOLD=0.2`
3. Verify your mic works: `rec -d test.wav` (SoX) or any recording app

### VAD triggers on background noise

Raise the threshold: `VAD_THRESHOLD=0.5` or higher.

### TTS audio is choppy / scrambled

This was caused by the ring buffer dropping samples when full. Fixed in commit `5da5585`. If you're on an older version, pull latest.

If still choppy, enable debug logging:
```bash
LOG_LEVEL=debug go run ./cmd/vox
```
Check `logs/tts_*.wav` — if the WAV sounds fine but speaker output doesn't, the issue is in the player.

### TTS audio stops after 1-2 sentences

The flush detection was misidentifying intermediate flushes as the final flush. Fixed in the same commit. Pull latest.

### Echo / feedback loop (TTS triggers VAD)

Echo suppression should handle this — VAD is disabled while `conv.Speaking` is true. If you're still getting echo:
- Use headphones (eliminates acoustic echo entirely)
- Check that `conv.Speaking` is being set/unset correctly in logs

### No sound from speakers

1. Check macOS audio output device
2. Verify TTS connection: look for `tts receive error` in stderr or logs
3. Test with a known working TTS model: `TTS_MODEL=aura-asteria-en`

## Connection Issues

### `websocket: bad handshake` (STT or TTS)

- Verify your API key is correct
- Check the model name exists (see [Deepgram STT models](https://developers.deepgram.com/docs/models), [TTS models](https://developers.deepgram.com/docs/tts-models))
- Some Aura-2 TTS models may require a paid plan. Try an Aura-1 model: `TTS_MODEL=aura-asteria-en`

### `groq returned 429` or rate limit errors

Groq free tier has rate limits. Wait a moment and try again, or use a different LLM:
```bash
LLM_BASE_URL=http://localhost:11434/v1
LLM_MODEL=llama3
LLM_API_KEY=ollama
```

### `deepgram connect: context deadline exceeded`

Network issue or Deepgram is down. Check your internet connection and [Deepgram status](https://status.deepgram.com).

## Transcription Issues

### Words cut off at the start of sentences

The pre-roll buffer should prevent this. If words are still clipped, the pre-roll might be too short. In `cmd/vox/main.go`, increase `prerollSize` from 10 to 15 (480ms of audio).

### Words repeated or duplicated

This can happen at chunk boundaries. The silence threshold (960ms) helps, but natural pauses within speech can still cause splits. Try increasing `silenceThreshold` from 30 to 40 (~1.3s).

### LLM response includes markdown

The system prompt tells the LLM to avoid markdown, and `stripMarkdown()` removes common formatting. If you still see markdown in TTS output, the LLM isn't following instructions. Try a different model or adjust the system prompt in `conversation.go`.

## Debugging

Enable full logging:
```bash
LOG_LEVEL=debug go run ./cmd/vox
```

This creates:
- `logs/vox_YYYYMMDD_HHMMSS.log` — full event trace
- `logs/tts_YYYYMMDD_HHMMSS.wav` — raw TTS audio (play with `afplay`)

Key things to look for in logs:
- `state` messages — verify correct transitions (LISTENING → THINKING → RESPONDING → LISTENING)
- `tts send` — verify sentences are being sent to TTS
- `tts flushed` with `finalFlushSent=true` — verify playback goroutine exits at the right time
- `ring buffer drained` — verify audio played completely
- `speech` with `state=RESPONDING` — interruption detected
