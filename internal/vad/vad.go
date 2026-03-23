package vad

import (
	"fmt"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	stateSize = 2 * 1 * 128 // 2 layers, batch 1, 128 units
)

// Detector runs Silero VAD inference using ONNX Runtime.
type Detector struct {
	session *ort.AdvancedSession

	// Input tensors
	audioInput *ort.Tensor[float32]
	srInput    *ort.Tensor[int64]
	stateInput *ort.Tensor[float32]

	// Output tensors
	probOutput  *ort.Tensor[float32]
	stateOutput *ort.Tensor[float32]

	Threshold float32
}

// Init sets up the ONNX Runtime environment. Call once at startup.
func Init(libPath string) error {
	ort.SetSharedLibraryPath(libPath)
	return ort.InitializeEnvironment()
}

// Shutdown cleans up the ONNX Runtime environment. Call once at exit.
func Shutdown() error {
	return ort.DestroyEnvironment()
}

// NewDetector creates a VAD detector from an ONNX model file.
func NewDetector(modelPath string, threshold float32) (*Detector, error) {
	d := &Detector{
		Threshold: threshold,
	}

	var err error

	// Input: audio chunk (batch=1, samples=512)
	d.audioInput, err = ort.NewTensor(ort.NewShape(1, 512), make([]float32, 512))
	if err != nil {
		return nil, fmt.Errorf("create audio tensor: %w", err)
	}

	// Input: sample rate (scalar)
	d.srInput, err = ort.NewTensor(ort.NewShape(1), []int64{16000})
	if err != nil {
		return nil, fmt.Errorf("create sr tensor: %w", err)
	}

	// Input: combined state (2, 1, 128) — zeroed initially
	d.stateInput, err = ort.NewTensor(ort.NewShape(2, 1, 128), make([]float32, stateSize))
	if err != nil {
		return nil, fmt.Errorf("create state tensor: %w", err)
	}

	// Output: speech probability (1, 1)
	d.probOutput, err = ort.NewEmptyTensor[float32](ort.NewShape(1, 1))
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}

	// Output: updated state (2, 1, 128)
	d.stateOutput, err = ort.NewEmptyTensor[float32](ort.NewShape(2, 1, 128))
	if err != nil {
		return nil, fmt.Errorf("create stateN tensor: %w", err)
	}

	// Create session with named inputs/outputs
	d.session, err = ort.NewAdvancedSession(
		modelPath,
		[]string{"input", "state", "sr"},
		[]string{"output", "stateN"},
		[]ort.Value{d.audioInput, d.stateInput, d.srInput},
		[]ort.Value{d.probOutput, d.stateOutput},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return d, nil
}

// Detect runs VAD on a single frame of float32 audio samples.
// Returns the speech probability (0.0 to 1.0).
func (d *Detector) Detect(samples []float32) (float32, error) {
	copy(d.audioInput.GetData(), samples)

	if err := d.session.Run(); err != nil {
		return 0, fmt.Errorf("vad inference: %w", err)
	}

	// Carry forward state for next call
	copy(d.stateInput.GetData(), d.stateOutput.GetData())

	return d.probOutput.GetData()[0], nil
}

// IsSpeech is a convenience — runs Detect and compares against threshold.
func (d *Detector) IsSpeech(samples []float32) (bool, float32, error) {
	prob, err := d.Detect(samples)
	if err != nil {
		return false, 0, err
	}
	return prob >= d.Threshold, prob, nil
}

// Reset clears the internal state (call between separate audio sessions).
func (d *Detector) Reset() {
	for i := range d.stateInput.GetData() {
		d.stateInput.GetData()[i] = 0
	}
}

// Destroy releases all ONNX resources.
func (d *Detector) Destroy() {
	if d.session != nil {
		d.session.Destroy()
	}
	d.audioInput.Destroy()
	d.srInput.Destroy()
	d.stateInput.Destroy()
	d.probOutput.Destroy()
	d.stateOutput.Destroy()
}
