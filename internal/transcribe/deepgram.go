package transcribe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const deepgramURL = "wss://api.deepgram.com/v1/listen"

type Deepgram struct {
	conn   *websocket.Conn
	apiKey string
}

// DeepgramResult is the transcription response from Deepgram.
type DeepgramResult struct {
	Type    string `json:"type"`
	IsFinal bool   `json:"is_final"`
	Channel struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"channel"`
}

func NewDeepgram(apiKey string) *Deepgram {
	return &Deepgram{apiKey: apiKey}
}

// Connect establishes a WebSocket connection with streaming params.
func (d *Deepgram) Connect(ctx context.Context) error {
	url := deepgramURL + "?model=nova-3&encoding=linear16&sample_rate=16000&interim_results=true&punctuate=true"

	header := http.Header{}
	header.Set("Authorization", "Token "+d.apiKey)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		return fmt.Errorf("deepgram connect: %w", err)
	}
	d.conn = conn
	return nil
}

// SendAudio sends raw PCM audio bytes as a binary WebSocket frame.
func (d *Deepgram) SendAudio(data []byte) error {
	return d.conn.WriteMessage(websocket.BinaryMessage, data)
}

// Receive reads the next transcription result from Deepgram.
func (d *Deepgram) Receive() (*DeepgramResult, error) {
	_, msg, err := d.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("deepgram read: %w", err)
	}

	var result DeepgramResult
	if err := json.Unmarshal(msg, &result); err != nil {
		return nil, fmt.Errorf("deepgram unmarshal: %w", err)
	}
	return &result, nil
}

// KeepAlive sends a keep-alive message to prevent timeout.
func (d *Deepgram) KeepAlive() error {
	msg := []byte(`{"type":"KeepAlive"}`)
	return d.conn.WriteMessage(websocket.TextMessage, msg)
}

// Close gracefully closes the Deepgram connection.
func (d *Deepgram) Close() error {
	if d.conn == nil {
		return nil
	}
	// Send close message
	d.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"Close"}`))
	return d.conn.Close()
}
