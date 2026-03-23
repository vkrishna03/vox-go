package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Addr         string
	STTProvider  string
	STTAPIKey    string
	STTModel     string
	TTSProvider  string
	TTSAPIKey    string
	TTSModel     string
	LLMBaseURL   string
	LLMAPIKey    string
	LLMModel     string
	VADThreshold float32
	LogLevel     string
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: no .env file found, using environment variables\n")
	}

	sttProvider := os.Getenv("STT_PROVIDER")
	if sttProvider == "" {
		sttProvider = "deepgram"
	}

	sttKey := os.Getenv("STT_API_KEY")
	if sttKey == "" {
		return nil, fmt.Errorf("STT_API_KEY env var is required")
	}

	sttModel := os.Getenv("STT_MODEL")
	if sttModel == "" {
		sttModel = "nova-3"
	}

	ttsProvider := os.Getenv("TTS_PROVIDER")
	if ttsProvider == "" {
		ttsProvider = "deepgram"
	}

	ttsKey := os.Getenv("TTS_API_KEY")
	if ttsKey == "" {
		ttsKey = sttKey // default: reuse STT key (same Deepgram account)
	}

	ttsModel := os.Getenv("TTS_MODEL")
	if ttsModel == "" {
		ttsModel = "aura-asteria-en"
	}

	llmKey := os.Getenv("LLM_API_KEY")
	if llmKey == "" {
		return nil, fmt.Errorf("LLM_API_KEY env var is required")
	}

	llmBaseURL := os.Getenv("LLM_BASE_URL")
	if llmBaseURL == "" {
		llmBaseURL = "https://api.groq.com/openai/v1"
	}

	llmModel := os.Getenv("LLM_MODEL")
	if llmModel == "" {
		llmModel = "llama-3.3-70b-versatile"
	}

	var vadThreshold float32 = 0.3
	if v := os.Getenv("VAD_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			vadThreshold = float32(f)
		}
	}

	logLevel := os.Getenv("LOG_LEVEL") // empty = disabled, "debug", "info", "warn", "error"

	return &Config{
		Addr:         ":8080",
		STTProvider:  sttProvider,
		STTAPIKey:    sttKey,
		STTModel:     sttModel,
		TTSProvider:  ttsProvider,
		TTSAPIKey:    ttsKey,
		TTSModel:     ttsModel,
		LLMBaseURL:   llmBaseURL,
		LLMAPIKey:    llmKey,
		LLMModel:     llmModel,
		VADThreshold: vadThreshold,
		LogLevel:     logLevel,
	}, nil
}
