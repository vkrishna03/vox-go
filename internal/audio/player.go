package audio

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
)

const (
	PlaybackSampleRate = 24000 // Deepgram TTS outputs 24kHz
	PlaybackFrameSize  = 512
)

type Player struct {
	stream *portaudio.Stream

	// Ring buffer for incoming audio data
	ring    []int16
	ringMu  sync.Mutex
	ringR   int // read position
	ringW   int // write position
	ringLen int // number of samples available
}

const ringSize = 24000 * 10 // 10 seconds of audio buffer at 24kHz

// NewPlayer opens a PortAudio output stream for speaker playback.
// PortAudio must already be initialized.
func NewPlayer() (*Player, error) {
	p := &Player{
		ring: make([]int16, ringSize),
	}

	stream, err := portaudio.OpenDefaultStream(0, Channels, float64(PlaybackSampleRate), PlaybackFrameSize, p.processAudio)
	if err != nil {
		return nil, fmt.Errorf("open output stream: %w", err)
	}
	p.stream = stream

	if err := stream.Start(); err != nil {
		return nil, fmt.Errorf("start output stream: %w", err)
	}

	return p, nil
}

// processAudio is the PortAudio callback. It fills the output buffer
// from the ring buffer. If not enough data, it outputs silence.
func (p *Player) processAudio(out []int16) {
	p.ringMu.Lock()
	defer p.ringMu.Unlock()

	for i := range out {
		if p.ringLen > 0 {
			out[i] = p.ring[p.ringR]
			p.ringR = (p.ringR + 1) % ringSize
			p.ringLen--
		} else {
			out[i] = 0
		}
	}
}

// Play queues raw PCM bytes (little-endian int16) for playback.
// Blocks if the ring buffer is full — creates backpressure so
// the TTS receive goroutine waits for the speaker to catch up.
func (p *Player) Play(data []byte) error {
	if len(data) < 2 {
		return nil
	}

	samples := make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}

	for len(samples) > 0 {
		p.ringMu.Lock()
		wrote := 0
		for _, s := range samples {
			if p.ringLen >= ringSize {
				break
			}
			p.ring[p.ringW] = s
			p.ringW = (p.ringW + 1) % ringSize
			p.ringLen++
			wrote++
		}
		p.ringMu.Unlock()

		samples = samples[wrote:]

		if len(samples) > 0 {
			// Buffer full — wait for speaker to consume some audio
			time.Sleep(10 * time.Millisecond)
		}
	}

	return nil
}

// Drain returns true if the ring buffer still has audio to play.
func (p *Player) Drain() bool {
	p.ringMu.Lock()
	defer p.ringMu.Unlock()
	return p.ringLen > 0
}

// Clear empties the ring buffer (used on interruption).
func (p *Player) Clear() {
	p.ringMu.Lock()
	defer p.ringMu.Unlock()
	p.ringR = 0
	p.ringW = 0
	p.ringLen = 0
}

func (p *Player) Close() error {
	if p.stream == nil {
		return nil
	}
	p.stream.Stop()
	return p.stream.Close()
}
