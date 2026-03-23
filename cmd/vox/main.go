package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/vkrishna03/vox-go/internal/audio"
	"github.com/vkrishna03/vox-go/internal/config"
	"github.com/vkrishna03/vox-go/internal/conversation"
	"github.com/vkrishna03/vox-go/internal/llm"
	"github.com/vkrishna03/vox-go/internal/logging"
	"github.com/vkrishna03/vox-go/internal/pipeline"
	"github.com/vkrishna03/vox-go/internal/transcribe"
	"github.com/vkrishna03/vox-go/internal/tts"
	"github.com/vkrishna03/vox-go/internal/tui"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Init components
	if err := vad.Init("/opt/homebrew/lib/libonnxruntime.dylib"); err != nil {
		return fmt.Errorf("onnx init: %w", err)
	}
	defer vad.Shutdown()

	detector, err := vad.NewDetector("models/silero_vad.onnx", cfg.VADThreshold)
	if err != nil {
		return fmt.Errorf("vad: %w", err)
	}
	defer detector.Destroy()

	rec, err := audio.NewRecorder()
	if err != nil {
		return fmt.Errorf("recorder: %w", err)
	}
	defer rec.Close()

	player, err := audio.NewPlayer()
	if err != nil {
		return fmt.Errorf("player: %w", err)
	}
	defer player.Close()

	stt, err := newTranscriber(cfg)
	if err != nil {
		return err
	}
	if err := stt.Connect(ctx); err != nil {
		return err
	}
	defer stt.Close()

	synth, err := newSynthesizer(cfg)
	if err != nil {
		return err
	}
	if err := synth.Connect(ctx); err != nil {
		return err
	}
	defer synth.Close()

	// Shared update channel for TUI
	updateCh := make(chan any, 64)

	// Wire everything together
	llmClient := llm.NewOpenAIClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
	conv := conversation.New(llmClient, synth, player, updateCh, cfg.DrainDelay)
	pipe := pipeline.New(rec, detector, stt, conv, cfg, updateCh)

	if err := rec.Start(); err != nil {
		return fmt.Errorf("start recording: %w", err)
	}

	var wg sync.WaitGroup

	// Goroutine 1: mic → VAD → STT
	wg.Add(1)
	go func() { defer wg.Done(); pipe.Run(ctx) }()

	// Goroutine 2: STT → transcriptCh
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
					logging.Logger.Error("stt receive", "err", err)
					return
				}
			}
			if result.Text != "" && result.IsFinal {
				conv.TranscriptCh <- result.Text
			}
		}
	}()

	// Goroutine 3: conversation state machine
	wg.Add(1)
	go func() { defer wg.Done(); conv.Run(ctx) }()

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

	// Run TUI (blocks until Ctrl+C)
	model := tui.NewModel(updateCh, cfg.VADThreshold)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		cancel()
		wg.Wait()
		return fmt.Errorf("tui: %w", err)
	}

	cancel()
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
