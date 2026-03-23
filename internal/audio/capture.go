package audio

import (
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

func (r *Recorder) Read() ([]int16, error) {
	if err := r.stream.Read(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}
	out := make([]int16, len(r.buf))
	copy(out, r.buf)
	return out, nil
}

func (r *Recorder) Close() error {
	r.stream.Stop()
	r.stream.Close()
	return portaudio.Terminate()
}
