package logging

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const logsDir = "logs"

var (
	Logger  *slog.Logger
	enabled bool
)

// Init sets up logging. level can be "debug", "info", "warn", "error", or empty (disabled).
func Init(level string) {
	level = strings.ToLower(strings.TrimSpace(level))

	if level == "" {
		enabled = false
		Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelError,
		}))
		return
	}

	enabled = true

	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	os.MkdirAll(logsDir, 0o755)

	path := filepath.Join(logsDir, fmt.Sprintf("vox_%s.log", time.Now().Format("20060102_150405")))
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create log file: %v\n", err)
		Logger = slog.Default()
		return
	}

	Logger = slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slogLevel,
	}))
	Logger.Info("logging enabled", "level", level, "path", path)
}

func Enabled() bool {
	return enabled
}

// AudioDumper writes raw PCM audio to a WAV file for debugging.
type AudioDumper struct {
	mu      sync.Mutex
	file    *os.File
	samples int
}

func NewAudioDumper(name string) (*AudioDumper, error) {
	os.MkdirAll(logsDir, 0o755)

	path := filepath.Join(logsDir, name)
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	// Placeholder WAV header — filled in at Close()
	f.Write(make([]byte, 44))

	return &AudioDumper{file: f}, nil
}

func (d *AudioDumper) Write(data []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.file.Write(data)
	d.samples += len(data) / 2
}

func (d *AudioDumper) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	dataSize := d.samples * 2
	fileSize := 36 + dataSize

	d.file.Seek(0, 0)

	var header [44]byte
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(fileSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], 24000)
	binary.LittleEndian.PutUint32(header[28:32], 48000)
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))

	d.file.Write(header[:])
	return d.file.Close()
}
