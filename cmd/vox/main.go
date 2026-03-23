package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/vkrishna03/vox-go/internal/audio"
	"github.com/vkrishna03/vox-go/internal/config"
	"github.com/vkrishna03/vox-go/internal/conversation"
	"github.com/vkrishna03/vox-go/internal/logging"
	"github.com/vkrishna03/vox-go/internal/llm"
	"github.com/vkrishna03/vox-go/internal/transcribe"
	"github.com/vkrishna03/vox-go/internal/tts"
	"github.com/vkrishna03/vox-go/internal/vad"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logging.Init(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Init ONNX Runtime
	if err := vad.Init("/opt/homebrew/lib/libonnxruntime.dylib"); err != nil {
		return fmt.Errorf("onnx init: %w", err)
	}
	defer vad.Shutdown()

	// Init VAD
	detector, err := vad.NewDetector("models/silero_vad.onnx", cfg.VADThreshold)
	if err != nil {
		return fmt.Errorf("vad: %w", err)
	}
	defer detector.Destroy()

	// Init mic
	rec, err := audio.NewRecorder()
	if err != nil {
		return fmt.Errorf("recorder: %w", err)
	}
	defer rec.Close()

	// Init speaker output
	player, err := audio.NewPlayer()
	if err != nil {
		return fmt.Errorf("player: %w", err)
	}
	defer player.Close()

	// Connect to STT provider
	stt, err := newTranscriber(cfg)
	if err != nil {
		return err
	}
	if err := stt.Connect(ctx); err != nil {
		return err
	}
	defer stt.Close()

	// Connect to TTS provider
	synth, err := newSynthesizer(cfg)
	if err != nil {
		return err
	}
	if err := synth.Connect(ctx); err != nil {
		return err
	}
	defer synth.Close()

	// Init LLM client and conversation
	llmClient := llm.NewOpenAIClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
	conv := conversation.New(llmClient, synth, player)

	fmt.Println("Listening... (Ctrl+C to stop)")
	fmt.Println()

	if err := rec.Start(); err != nil {
		return fmt.Errorf("start recording: %w", err)
	}

	var wg sync.WaitGroup

	// Goroutine 1: mic → VAD → STT + speechCh signaling
	wg.Add(1)
	go func() {
		defer wg.Done()

		speaking := false
		silenceFrames := 0
		const silenceThreshold = 30 // ~960ms

		const prerollSize = 10
		preroll := make([][]int16, 0, prerollSize)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			samples, err := rec.Read()
			if err != nil {
				fmt.Fprintf(os.Stderr, "mic read error: %v\n", err)
				return
			}

			floats := audio.Int16ToFloat32(samples)
			isSpeech, _, err := detector.IsSpeech(floats)
			if err != nil {
				fmt.Fprintf(os.Stderr, "vad error: %v\n", err)
				return
			}

			// Echo suppression: ignore VAD while TTS is playing through speaker.
			// Still read mic frames to keep PortAudio buffer flowing.
			if conv.Speaking {
				speaking = false
				silenceFrames = 0
				preroll = preroll[:0]
				continue
			}

			if isSpeech {
				silenceFrames = 0
				if !speaking {
					speaking = true
					select {
					case conv.SpeechCh <- true:
					default:
					}
					for _, old := range preroll {
						stt.SendAudio(audio.Int16ToBytes(old))
					}
					preroll = preroll[:0]
				}
				if err := stt.SendAudio(audio.Int16ToBytes(samples)); err != nil {
					fmt.Fprintf(os.Stderr, "send error: %v\n", err)
					return
				}
			} else if speaking {
				silenceFrames++
				stt.SendAudio(audio.Int16ToBytes(samples))

				if silenceFrames >= silenceThreshold {
					speaking = false
					select {
					case conv.SpeechCh <- false:
					default:
					}
				}
			} else {
				if len(preroll) >= prerollSize {
					preroll = preroll[1:]
				}
				preroll = append(preroll, samples)
			}
		}
	}()

	// Goroutine 2: transcriber → transcriptCh
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			result, err := stt.Receive()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					fmt.Fprintf(os.Stderr, "receive error: %v\n", err)
					return
				}
			}

			if result.Text == "" {
				continue
			}

			if result.IsFinal {
				conv.TranscriptCh <- result.Text
			}
		}
	}()

	// Goroutine 3: conversation state machine
	wg.Add(1)
	go func() {
		defer wg.Done()
		conv.Run(ctx)
	}()

	// Goroutine 4: STT keep-alive
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stt.KeepAlive()
			}
		}
	}()

	<-ctx.Done()
	fmt.Println("\nStopping...")
	wg.Wait()
	return nil
}

func newTranscriber(cfg *config.Config) (transcribe.Transcriber, error) {
	switch cfg.STTProvider {
	case "deepgram":
		return transcribe.NewDeepgram(cfg.STTAPIKey, cfg.STTModel), nil
	default:
		return nil, fmt.Errorf("unknown STT provider: %s", cfg.STTProvider)
	}
}

func newSynthesizer(cfg *config.Config) (tts.Synthesizer, error) {
	switch cfg.TTSProvider {
	case "deepgram":
		return tts.NewDeepgramTTS(cfg.TTSAPIKey, cfg.TTSModel), nil
	default:
		return nil, fmt.Errorf("unknown TTS provider: %s", cfg.TTSProvider)
	}
}
