package audio

import "encoding/binary"

// Int16ToBytes converts int16 PCM samples to little-endian bytes.
func Int16ToBytes(samples []int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
	}
	return out
}

// Int16ToFloat32 converts int16 PCM samples to float32 in [-1.0, 1.0].
func Int16ToFloat32(samples []int16) []float32 {
	out := make([]float32, len(samples))
	for i, s := range samples {
		out[i] = float32(s) / 32768.0
	}
	return out
}
