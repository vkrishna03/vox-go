package conversation

import (
	"context"
	"strings"
	"time"

	"github.com/vkrishna03/vox-go/internal/audio"
	"github.com/vkrishna03/vox-go/internal/llm"
	"github.com/vkrishna03/vox-go/internal/logging"
	"github.com/vkrishna03/vox-go/internal/tts"
	"github.com/vkrishna03/vox-go/internal/tui"
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

var systemPrompt = `You are a helpful voice assistant. Keep responses concise and conversational.
IMPORTANT: Your responses will be spoken aloud via text-to-speech.
Do NOT use markdown formatting (no **, no ##, no bullet points, no numbered lists).
Use plain, natural spoken language only.`

type Conversation struct {
	state   State
	history []llm.Message
	client  llm.Streamer
	synth   tts.Synthesizer
	player  *audio.Player

	SpeechCh     chan bool
	TranscriptCh chan string
	thinkDoneCh  chan string

	cancelResponse context.CancelFunc
	Speaking       bool
	pendingText    strings.Builder

	UpdateCh   chan any
	DrainDelay time.Duration
}

func New(client llm.Streamer, synth tts.Synthesizer, player *audio.Player, updateCh chan any, drainDelay time.Duration) *Conversation {
	return &Conversation{
		state:  Listening,
		client: client,
		synth:  synth,
		player: player,
		history: []llm.Message{
			{Role: "system", Content: systemPrompt},
		},
		SpeechCh:     make(chan bool, 1),
		TranscriptCh: make(chan string, 8),
		thinkDoneCh:  make(chan string, 1),
		UpdateCh:     updateCh,
		DrainDelay:   drainDelay,
	}
}

func (c *Conversation) send(msg any) {
	select {
	case c.UpdateCh <- msg:
	default:
	}
}

func (c *Conversation) setState(s State) {
	c.state = s
	logging.Logger.Info("state", "to", s.String())
	c.send(tui.StateMsg{State: s.String()})
}

// Run is the main state machine loop.
func (c *Conversation) Run(ctx context.Context) {
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
				logging.Logger.Debug("transcript", "text", text)
			}

		case speaking := <-c.SpeechCh:
			logging.Logger.Debug("speech", "speaking", speaking, "state", c.state.String())
			switch c.state {
			case Listening:
				if !speaking && c.pendingText.Len() > 0 {
					drainTimer = time.NewTimer(c.DrainDelay)
					drainCh = drainTimer.C
				}
			case Responding:
				if speaking {
					if c.cancelResponse != nil {
						c.cancelResponse()
					}
					if c.player != nil {
						c.player.Clear()
					}
					c.Speaking = false
					logging.Logger.Info("interrupted")
					c.send(tui.InfoMsg{Text: "[interrupted]"})
				}
			}

		case <-drainCh:
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
				c.send(tui.TranscriptMsg{Text: userText})
				c.think(ctx, userText)
			}

		case response := <-c.thinkDoneCh:
			if response != "" {
				c.history = append(c.history, llm.Message{Role: "assistant", Content: response})
			}
			c.setState(Listening)
			c.Speaking = false
			c.send(tui.ResponseDoneMsg{})
		}
	}
}
