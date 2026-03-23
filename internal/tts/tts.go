package tts

import (
	"context"
	"errors"
)

// ErrFlushed is returned by Receive when TTS has finished sending
// all audio for the current flush. Not a real error — it's a signal.
var ErrFlushed = errors.New("tts: flushed")

// Synthesizer is the interface any TTS provider must implement.
type Synthesizer interface {
	Connect(ctx context.Context) error
	SendText(text string) error
	Flush() error
	Receive() ([]byte, error) // returns raw PCM audio bytes, or ErrFlushed
	Close() error
}
