package conversation

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vkrishna03/vox-go/internal/audio"
	"github.com/vkrishna03/vox-go/internal/logging"
	"github.com/vkrishna03/vox-go/internal/llm"
	"github.com/vkrishna03/vox-go/internal/tts"
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
}

func New(client llm.Streamer, synth tts.Synthesizer, player *audio.Player) *Conversation {
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
	}
}

func (c *Conversation) setState(s State) {
	c.state = s
	logging.Logger.Info("state", "to", s.String())
}

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
					drainTimer = time.NewTimer(500 * time.Millisecond)
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
					fmt.Print("\n[interrupted]\n\n")
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
				fmt.Printf("> %s\n\n", userText)
				c.think(ctx, userText)
			}

		case response := <-c.thinkDoneCh:
			if response != "" {
				c.history = append(c.history, llm.Message{Role: "assistant", Content: response})
			}
			c.setState(Listening)
			c.Speaking = false
			fmt.Print("\n\n")
		}
	}
}

func (c *Conversation) think(ctx context.Context, userText string) {
	c.setState(Thinking)
	c.history = append(c.history, llm.Message{Role: "user", Content: userText})

	fmt.Print("[thinking...] ")

	responseCtx, cancel := context.WithCancel(ctx)
	c.cancelResponse = cancel

	tokenCh, errCh := c.client.Stream(responseCtx, c.history)

	go func() {
		var response strings.Builder
		var sentenceBuf strings.Builder
		first := true
		sentenceCount := 0

		var playbackWg sync.WaitGroup
		var finalFlushSent atomic.Bool

		// Audio dumper for debugging
		var dumper *logging.AudioDumper
		if logging.Enabled() {
			var err error
			dumper, err = logging.NewAudioDumper(fmt.Sprintf("tts_%s.wav", time.Now().Format("20060102_150405")))
			if err != nil {
				logging.Logger.Error("audio dumper", "err", err)
			}
		}

		if c.synth != nil && c.player != nil {
			playbackWg.Add(1)
			go func() {
				defer playbackWg.Done()
				totalBytes := 0
				chunks := 0
				for {
					data, err := c.synth.Receive()
					if err != nil {
						if err == tts.ErrFlushed {
							logging.Logger.Debug("tts flushed", "finalFlushSent", finalFlushSent.Load(), "totalBytes", totalBytes, "chunks", chunks)
							if finalFlushSent.Load() {
								return
							}
							continue
						}
						logging.Logger.Error("tts receive", "err", err)
						fmt.Fprintf(os.Stderr, "\ntts receive error: %v\n", err)
						return
					}
					if len(data) > 0 {
						totalBytes += len(data)
						chunks++
						c.player.Play(data)
						if dumper != nil {
							dumper.Write(data)
						}
					}
				}
			}()
		}

		for token := range tokenCh {
			if first {
				fmt.Print("\r\033[K")
				c.setState(Responding)
				c.Speaking = true
				first = false
			}
			fmt.Print(token)
			response.WriteString(token)
			sentenceBuf.WriteString(token)

			if c.synth != nil && isSentenceEnd(sentenceBuf.String()) {
				text := stripMarkdown(sentenceBuf.String())
				if text != "" {
					sentenceCount++
					logging.Logger.Debug("tts send", "sentence", sentenceCount, "len", len(text), "text", text)
					c.synth.SendText(text)
				}
				sentenceBuf.Reset()
			}
		}

		logging.Logger.Debug("llm done", "sentences_sent", sentenceCount)

		if c.synth != nil {
			remaining := stripMarkdown(sentenceBuf.String())
			if remaining != "" {
				logging.Logger.Debug("tts send remaining", "len", len(remaining), "text", remaining)
				c.synth.SendText(remaining)
			}
			logging.Logger.Debug("sending final flush")
			finalFlushSent.Store(true)
			c.synth.Flush()
		}

		if c.synth != nil && c.player != nil {
			logging.Logger.Debug("waiting for playback to finish")
			playbackWg.Wait()
			logging.Logger.Debug("playback goroutine done, draining ring buffer")

			for c.player.Drain() {
				time.Sleep(50 * time.Millisecond)
			}
			logging.Logger.Debug("ring buffer drained")
		}

		if dumper != nil {
			dumper.Close()
			logging.Logger.Info("audio dumped to file")
		}

		c.Speaking = false

		if err := <-errCh; err != nil && ctx.Err() == nil && err != context.Canceled {
			fmt.Printf("\n[error: %v]\n", err)
		}

		c.thinkDoneCh <- response.String()
	}()
}

func isSentenceEnd(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	last := s[len(s)-1]
	return last == '.' || last == '!' || last == '?' || last == ':' || last == ';' || last == '\n'
}

var markdownRe = regexp.MustCompile(`[*_#\[\]()~` + "`" + `>|]`)

func stripMarkdown(s string) string {
	s = markdownRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "  ", " ")
	return strings.TrimSpace(s)
}
