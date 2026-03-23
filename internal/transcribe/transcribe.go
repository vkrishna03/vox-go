package transcribe

import "context"

// Result is a provider-agnostic transcription result.
type Result struct {
	Text    string
	IsFinal bool
}

// Transcriber is the interface any STT provider must implement.
type Transcriber interface {
	Connect(ctx context.Context) error
	SendAudio(data []byte) error
	Receive() (*Result, error)
	KeepAlive() error
	Close() error
}
