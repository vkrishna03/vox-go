# Configuration

All configuration is via environment variables. Use a `.env` file for convenience (loaded by godotenv).

```bash
cp .env.example .env
```

## Environment Variables

### Speech-to-Text (STT)

| Variable | Required | Default | Description |
|---|---|---|---|
| `STT_PROVIDER` | No | `deepgram` | STT provider name |
| `STT_API_KEY` | Yes | — | API key for the STT provider |
| `STT_MODEL` | No | `nova-3` | STT model to use |

Deepgram models: `nova-3` (latest, recommended), `nova-2`, `enhanced`, `base`. See [Deepgram models](https://developers.deepgram.com/docs/models).

### Text-to-Speech (TTS)

| Variable | Required | Default | Description |
|---|---|---|---|
| `TTS_PROVIDER` | No | `deepgram` | TTS provider name |
| `TTS_API_KEY` | No | Same as `STT_API_KEY` | API key for TTS (defaults to STT key if same provider) |
| `TTS_MODEL` | No | `aura-asteria-en` | TTS voice model |

Deepgram voices (Aura 2, recommended):
- `aura-2-thalia-en` — natural female
- `aura-2-athena-en` — clear female
- `aura-2-zeus-en` — deep male
- `aura-2-orion-en` — natural male
- `aura-2-luna-en` — warm female

Full list: [Deepgram TTS models](https://developers.deepgram.com/docs/tts-models)

Note: Some Aura-2 models may require a paid Deepgram plan. Aura-1 models (e.g. `aura-asteria-en`) work on the free tier.

### LLM

| Variable | Required | Default | Description |
|---|---|---|---|
| `LLM_API_KEY` | Yes | — | API key for the LLM provider |
| `LLM_BASE_URL` | No | `https://api.groq.com/openai/v1` | OpenAI-compatible API base URL |
| `LLM_MODEL` | No | `llama-3.3-70b-versatile` | LLM model name |

Common configurations:

```bash
# Groq (default) — fast, free tier
LLM_BASE_URL=https://api.groq.com/openai/v1
LLM_MODEL=llama-3.3-70b-versatile

# OpenAI
LLM_BASE_URL=https://api.openai.com/v1
LLM_MODEL=gpt-4o

# Ollama (local, no API key needed)
LLM_BASE_URL=http://localhost:11434/v1
LLM_MODEL=llama3
LLM_API_KEY=ollama  # any non-empty string

# Together AI
LLM_BASE_URL=https://api.together.xyz/v1
LLM_MODEL=meta-llama/Llama-3-70b-chat-hf
```

### Voice Activity Detection (VAD)

| Variable | Required | Default | Description |
|---|---|---|---|
| `VAD_THRESHOLD` | No | `0.3` | Speech detection sensitivity (0.0–1.0) |

- **Lower** (e.g. `0.2`): more sensitive, picks up quiet speech, more false positives from background noise
- **Higher** (e.g. `0.5`): needs louder speech, fewer false triggers
- Start at `0.3` and adjust based on your mic and environment

### Logging

| Variable | Required | Default | Description |
|---|---|---|---|
| `LOG_LEVEL` | No | (disabled) | Logging level: `debug`, `info`, `warn`, `error` |

When set, logs are written to `logs/vox_YYYYMMDD_HHMMSS.log`. At `debug` level, TTS audio is also saved to `logs/tts_YYYYMMDD_HHMMSS.wav` for inspection.

Log levels:
- `debug` — everything: state transitions, VAD events, TTS chunks, flush events, audio stats
- `info` — state transitions, audio dump notifications
- `warn` — warnings only
- `error` — errors only
- (empty/unset) — logging disabled, errors only go to stderr

## Example .env

```bash
# Speech-to-text
STT_PROVIDER=deepgram
STT_API_KEY=your_deepgram_key
STT_MODEL=nova-3

# Text-to-speech
TTS_PROVIDER=deepgram
TTS_MODEL=aura-2-thalia-en

# LLM
LLM_API_KEY=your_groq_key
LLM_BASE_URL=https://api.groq.com/openai/v1
LLM_MODEL=llama-3.3-70b-versatile

# VAD
VAD_THRESHOLD=0.3

# Logging (comment out to disable)
# LOG_LEVEL=debug
```
