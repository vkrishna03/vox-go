package llm

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Streamer is the interface any LLM provider must implement.
// Stream sends messages and returns a channel of tokens and a channel of errors.
// Cancelling ctx must stop the stream.
type Streamer interface {
	Stream(ctx context.Context, messages []Message) (<-chan string, <-chan error)
}
