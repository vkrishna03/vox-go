package pipeline

import (
	"context"

	"github.com/vkrishna03/vox-go/internal/audio"
	"github.com/vkrishna03/vox-go/internal/config"
	"github.com/vkrishna03/vox-go/internal/conversation"
	"github.com/vkrishna03/vox-go/internal/logging"
	"github.com/vkrishna03/vox-go/internal/transcribe"
	"github.com/vkrishna03/vox-go/internal/tui"
	"github.com/vkrishna03/vox-go/internal/vad"
)

// Pipeline runs the mic → VAD → STT loop and signals the conversation.
type Pipeline struct {
	recorder *audio.Recorder
	detector *vad.Detector
	stt      transcribe.Transcriber
	conv     *conversation.Conversation
	cfg      *config.Config
	updateCh chan any
}

func New(
	rec *audio.Recorder,
	det *vad.Detector,
	stt transcribe.Transcriber,
	conv *conversation.Conversation,
	cfg *config.Config,
	updateCh chan any,
) *Pipeline {
	return &Pipeline{
		recorder: rec,
		detector: det,
		stt:      stt,
		conv:     conv,
		cfg:      cfg,
		updateCh: updateCh,
	}
}

// Run captures audio, runs VAD, sends speech to STT, and signals the conversation.
func (p *Pipeline) Run(ctx context.Context) {
	speaking := false
	silenceFrames := 0
	preroll := make([][]int16, 0, p.cfg.PrerollSize)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		samples, err := p.recorder.Read()
		if err != nil {
			logging.Logger.Error("mic read", "err", err)
			return
		}

		floats := audio.Int16ToFloat32(samples)
		prob, err := p.detector.Detect(floats)
		if err != nil {
			logging.Logger.Error("vad", "err", err)
			return
		}

		// Send audio levels to TUI
		threshold := p.cfg.VADThreshold
		if p.conv.Speaking {
			threshold = p.cfg.VADThresholdBump
		}

		level := tui.RMS(samples)
		select {
		case p.updateCh <- tui.AudioMsg{Level: level, VADProb: prob, Threshold: threshold}:
		default:
		}

		isSpeech := prob >= threshold

		if isSpeech {
			silenceFrames = 0
			if !speaking {
				speaking = true
				select {
				case p.conv.SpeechCh <- true:
				default:
				}
				// Flush pre-roll buffer
				for _, old := range preroll {
					p.stt.SendAudio(audio.Int16ToBytes(old))
				}
				preroll = preroll[:0]
			}
			if err := p.stt.SendAudio(audio.Int16ToBytes(samples)); err != nil {
				logging.Logger.Error("stt send", "err", err)
				return
			}
		} else if speaking {
			silenceFrames++
			p.stt.SendAudio(audio.Int16ToBytes(samples))

			if silenceFrames >= p.cfg.SilenceFrames {
				speaking = false
				select {
				case p.conv.SpeechCh <- false:
				default:
				}
			}
		} else {
			// Fill pre-roll buffer
			if len(preroll) >= p.cfg.PrerollSize {
				preroll = preroll[1:]
			}
			preroll = append(preroll, samples)
		}
	}
}
