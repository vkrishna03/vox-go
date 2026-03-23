package conversation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vkrishna03/vox-go/internal/llm"
	"github.com/vkrishna03/vox-go/internal/logging"
	"github.com/vkrishna03/vox-go/internal/tts"
	"github.com/vkrishna03/vox-go/internal/tui"
)

// think transitions to THINKING and spawns the LLM + TTS goroutines.
func (c *Conversation) think(ctx context.Context, userText string) {
	c.setState(Thinking)
	c.history = append(c.history, llm.Message{Role: "user", Content: userText})

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

		var dumper *logging.AudioDumper
		if logging.Enabled() {
			var err error
			dumper, err = logging.NewAudioDumper(fmt.Sprintf("tts_%s.wav", time.Now().Format("20060102_150405")))
			if err != nil {
				logging.Logger.Error("audio dumper", "err", err)
			}
		}

		// Playback goroutine: TTS audio → ring buffer → speaker
		if c.synth != nil && c.player != nil {
			playbackWg.Add(1)
			go func() {
				defer playbackWg.Done()
				for {
					data, err := c.synth.Receive()
					if err != nil {
						if err == tts.ErrFlushed {
							logging.Logger.Debug("tts flushed", "finalFlushSent", finalFlushSent.Load())
							if finalFlushSent.Load() {
								return
							}
							continue
						}
						logging.Logger.Error("tts receive", "err", err)
						return
					}
					if len(data) > 0 {
						c.player.Play(data)
						if dumper != nil {
							dumper.Write(data)
						}
					}
				}
			}()
		}

		// Read LLM tokens, print them, buffer sentences for TTS
		for token := range tokenCh {
			if first {
				c.setState(Responding)
				c.Speaking = true
				first = false
			}
			c.send(tui.TokenMsg{Token: token})
			response.WriteString(token)
			sentenceBuf.WriteString(token)

			if c.synth != nil && isSentenceEnd(sentenceBuf.String()) {
				text := stripMarkdown(sentenceBuf.String())
				if text != "" {
					sentenceCount++
					logging.Logger.Debug("tts send", "sentence", sentenceCount, "len", len(text))
					c.synth.SendText(text)
				}
				sentenceBuf.Reset()
			}
		}

		logging.Logger.Debug("llm done", "sentences_sent", sentenceCount)

		// Send remaining text and flush TTS
		if c.synth != nil {
			remaining := stripMarkdown(sentenceBuf.String())
			if remaining != "" {
				c.synth.SendText(remaining)
			}
			finalFlushSent.Store(true)
			c.synth.Flush()
		}

		// Wait for all audio to play through speaker
		if c.synth != nil && c.player != nil {
			playbackWg.Wait()
			for c.player.Drain() {
				time.Sleep(50 * time.Millisecond)
			}
		}

		if dumper != nil {
			dumper.Close()
		}

		c.Speaking = false

		if err := <-errCh; err != nil && ctx.Err() == nil && err != context.Canceled {
			c.send(tui.InfoMsg{Text: fmt.Sprintf("[error: %v]", err)})
		}

		c.thinkDoneCh <- response.String()
	}()
}
