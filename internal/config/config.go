package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Addr           string
	DeepgramAPIKey string
}

func Load() (*Config, error) {
	godotenv.Load() // .env is optional, no error if missing

	key := os.Getenv("DEEPGRAM_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("DEEPGRAM_API_KEY env var is required")
	}

	return &Config{
		Addr:           ":8080",
		DeepgramAPIKey: key,
	}, nil
}
