package audio

import (
	"encoding/binary"
	"fmt"

	"github.com/gordonklaus/portaudio"
)

const (
	SampleRate = 16000
	Channels   = 1
	FrameSize  = 512 // 32ms at 16kHz — matches Silero VAD requirement
)

type Recorder struct {
	stream *portaudio.Stream
	buf    []int16
}

func NewRecorder() (*Recorder, error) {
	if err := portaudio.Initialize(); err != nil {
		return nil, fmt.Errorf("portaudio init: %w", err)
	}

	r := &Recorder{
		buf: make([]int16, FrameSize),
	}

	stream, err := portaudio.OpenDefaultStream(Channels, 0, float64(SampleRate), FrameSize, r.buf)
	if err != nil {
		portaudio.Terminate()
		return nil, fmt.Errorf("open stream: %w", err)
	}
	r.stream = stream

	return r, nil
}

func (r *Recorder) Start() error {
	return r.stream.Start()
}

// Read fills the internal buffer with one frame from the mic.
// Returns the raw int16 samples — used by VAD (as float32) and Deepgram (as bytes).
func (r *Recorder) Read() ([]int16, error) {
	if err := r.stream.Read(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}
	out := make([]int16, len(r.buf))
	copy(out, r.buf)
	return out, nil
}

// Int16ToBytes converts int16 PCM samples to little-endian bytes for Deepgram.
func Int16ToBytes(samples []int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
	}
	return out
}

// Int16ToFloat32 converts int16 PCM samples to float32 in [-1.0, 1.0] for Silero VAD.
func Int16ToFloat32(samples []int16) []float32 {
	out := make([]float32, len(samples))
	for i, s := range samples {
		out[i] = float32(s) / 32768.0
	}
	return out
}

func (r *Recorder) Close() error {
	r.stream.Stop()
	r.stream.Close()
	return portaudio.Terminate()
}
