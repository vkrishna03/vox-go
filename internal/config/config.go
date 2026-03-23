package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Addr             string
	STTProvider      string
	STTAPIKey        string
	STTModel         string
	TTSProvider      string
	TTSAPIKey        string
	TTSModel         string
	LLMBaseURL       string
	LLMAPIKey        string
	LLMModel         string
	VADThreshold     float32
	VADThresholdBump float32
	SilenceFrames    int
	PrerollSize      int
	DrainDelay       time.Duration
	LogLevel         string
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
		ttsKey = sttKey
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

	vadThreshold := envFloat32("VAD_THRESHOLD", 0.3)
	vadThresholdBump := envFloat32("VAD_THRESHOLD_BUMP", 0.7)
	silenceFrames := envInt("SILENCE_FRAMES", 30)
	prerollSize := envInt("PREROLL_SIZE", 10)
	drainDelay := time.Duration(envInt("DRAIN_DELAY_MS", 500)) * time.Millisecond

	logLevel := os.Getenv("LOG_LEVEL")

	return &Config{
		Addr:             ":8080",
		STTProvider:      sttProvider,
		STTAPIKey:        sttKey,
		STTModel:         sttModel,
		TTSProvider:      ttsProvider,
		TTSAPIKey:        ttsKey,
		TTSModel:         ttsModel,
		LLMBaseURL:       llmBaseURL,
		LLMAPIKey:        llmKey,
		LLMModel:         llmModel,
		VADThreshold:     vadThreshold,
		VADThresholdBump: vadThresholdBump,
		SilenceFrames:    silenceFrames,
		PrerollSize:      prerollSize,
		DrainDelay:       drainDelay,
		LogLevel:         logLevel,
	}, nil
}

func envFloat32(key string, fallback float32) float32 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			return float32(f)
		}
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
