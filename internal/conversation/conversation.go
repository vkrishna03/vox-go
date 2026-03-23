package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vkrishna03/vox-go/internal/llm"
)

type State int

const (
	Listening  State = iota
	Thinking
	Responding
)

func (s State) String() string {
	switch s {
	case Listening:
		return "LISTENING"
	case Thinking:
		return "THINKING"
	case Responding:
		return "RESPONDING"
	default:
		return "UNKNOWN"
	}
}

type Conversation struct {
	state   State
	history []llm.Message
	client  llm.Streamer

	// Channels — the nervous system
	SpeechCh     chan bool   // true=speech started, false=speech ended
	TranscriptCh chan string // final transcripts from Deepgram
	thinkDoneCh  chan string // assistant response (full or partial) from think goroutine

	// Cancellation for the current LLM response
	cancelResponse context.CancelFunc

	// Accumulated text while listening
	pendingText strings.Builder
}

func New(client llm.Streamer) *Conversation {
	return &Conversation{
		state:  Listening,
		client: client,
		history: []llm.Message{
			{Role: "system", Content: "You are a helpful voice assistant. Keep responses concise and conversational."},
		},
		SpeechCh:     make(chan bool, 1),
		TranscriptCh: make(chan string, 8),
		thinkDoneCh:  make(chan string, 1),
	}
}

// Run is the main state machine loop. It reads from channels and manages transitions.
func (c *Conversation) Run(ctx context.Context) {
	// Timer for collecting late transcripts after speech ends
	var drainTimer *time.Timer
	var drainCh <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return

		case text := <-c.TranscriptCh:
			if c.state == Listening {
				c.pendingText.WriteString(text)
				c.pendingText.WriteString(" ")
			}

		case speaking := <-c.SpeechCh:
			switch c.state {
			case Listening:
				if !speaking && c.pendingText.Len() > 0 {
					// Speech ended with accumulated text.
					// Wait briefly for any late Deepgram finals.
					drainTimer = time.NewTimer(500 * time.Millisecond)
					drainCh = drainTimer.C
				}
			case Responding:
				if speaking {
					// Interruption! Cancel the LLM stream.
					if c.cancelResponse != nil {
						c.cancelResponse()
					}
					fmt.Print("\n[interrupted]\n\n")
				}
			}

		case <-drainCh:
			// Drain timer fired — collect any remaining transcripts and transition
			drainCh = nil
		drainLoop:
			for {
				select {
				case text := <-c.TranscriptCh:
					c.pendingText.WriteString(text)
					c.pendingText.WriteString(" ")
				default:
					break drainLoop
				}
			}

			userText := strings.TrimSpace(c.pendingText.String())
			c.pendingText.Reset()

			if userText != "" {
				fmt.Printf("> %s\n\n", userText)
				c.think(ctx, userText)
			}

		case response := <-c.thinkDoneCh:
			// think goroutine finished (completed or interrupted)
			if response != "" {
				c.history = append(c.history, llm.Message{Role: "assistant", Content: response})
			}
			c.state = Listening
			fmt.Print("\n\n")
		}
	}
}

// think transitions to THINKING state and spawns the LLM goroutine.
func (c *Conversation) think(ctx context.Context, userText string) {
	c.state = Thinking
	c.history = append(c.history, llm.Message{Role: "user", Content: userText})

	fmt.Print("[thinking...] ")

	// Create a cancellable child context for this response
	responseCtx, cancel := context.WithCancel(ctx)
	c.cancelResponse = cancel

	tokenCh, errCh := c.client.Stream(responseCtx, c.history)

	// Spawn goroutine to read tokens and print them
	go func() {
		var response strings.Builder
		first := true

		for token := range tokenCh {
			if first {
				// Clear the [thinking...] indicator
				fmt.Print("\r\033[K")
				c.state = Responding
				first = false
			}
			fmt.Print(token)
			response.WriteString(token)
		}

		// Check for errors (ignore context cancelled — that's interruption)
		if err := <-errCh; err != nil && ctx.Err() == nil && err != context.Canceled {
			fmt.Printf("\n[error: %v]\n", err)
		}

		c.thinkDoneCh <- response.String()
	}()
}
