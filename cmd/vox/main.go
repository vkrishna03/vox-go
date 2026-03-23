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
	"github.com/vkrishna03/vox-go/internal/transcribe"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Init ONNX Runtime
	if err := vad.Init("/opt/homebrew/lib/libonnxruntime.dylib"); err != nil {
		return fmt.Errorf("onnx init: %w", err)
	}
	defer vad.Shutdown()

	// Init VAD
	detector, err := vad.NewDetector("models/silero_vad.onnx", 0.5)
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

	// Connect to Deepgram
	dg := transcribe.NewDeepgram(cfg.DeepgramAPIKey)
	if err := dg.Connect(ctx); err != nil {
		return err
	}
	defer dg.Close()

	fmt.Println("Listening... (Ctrl+C to stop)")
	fmt.Println()

	if err := rec.Start(); err != nil {
		return fmt.Errorf("start recording: %w", err)
	}

	var wg sync.WaitGroup

	// Goroutine: mic → VAD → deepgram
	wg.Add(1)
	go func() {
		defer wg.Done()

		speaking := false
		silenceFrames := 0
		// Number of silent frames before we consider speech ended.
		// 30 frames * 32ms = ~960ms of silence to end a phrase.
		const silenceThreshold = 30

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

			if isSpeech {
				silenceFrames = 0
				speaking = true
				bytes := audio.Int16ToBytes(samples)
				if err := dg.SendAudio(bytes); err != nil {
					fmt.Fprintf(os.Stderr, "send error: %v\n", err)
					return
				}
			} else if speaking {
				silenceFrames++
				// Keep sending during short pauses to avoid chopping phrases
				bytes := audio.Int16ToBytes(samples)
				dg.SendAudio(bytes)

				if silenceFrames >= silenceThreshold {
					speaking = false
				}
			}
		}
	}()

	// Goroutine: deepgram → terminal
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			result, err := dg.Receive()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					fmt.Fprintf(os.Stderr, "receive error: %v\n", err)
					return
				}
			}

			if result.Type != "Results" {
				continue
			}
			if len(result.Channel.Alternatives) == 0 {
				continue
			}

			text := result.Channel.Alternatives[0].Transcript
			if text == "" {
				continue
			}

			if result.IsFinal {
				fmt.Printf("\r\033[K> %s\n", text)
			} else {
				fmt.Printf("\r\033[K  %s", text)
			}
		}
	}()

	// Goroutine: keep-alive every 5s
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
				dg.KeepAlive()
			}
		}
	}()

	<-ctx.Done()
	fmt.Println("\nStopping...")
	wg.Wait()
	return nil
}
