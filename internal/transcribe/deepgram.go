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
	model  string
}

// deepgramResult is the raw JSON response from Deepgram.
type deepgramResult struct {
	Type    string `json:"type"`
	IsFinal bool   `json:"is_final"`
	Channel struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"channel"`
}

func NewDeepgram(apiKey, model string) *Deepgram {
	return &Deepgram{apiKey: apiKey, model: model}
}

func (d *Deepgram) Connect(ctx context.Context) error {
	url := fmt.Sprintf("%s?model=%s&encoding=linear16&sample_rate=16000&interim_results=true&punctuate=true", deepgramURL, d.model)

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

func (d *Deepgram) SendAudio(data []byte) error {
	return d.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (d *Deepgram) Receive() (*Result, error) {
	_, msg, err := d.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("deepgram read: %w", err)
	}

	var raw deepgramResult
	if err := json.Unmarshal(msg, &raw); err != nil {
		return nil, fmt.Errorf("deepgram unmarshal: %w", err)
	}

	// Skip non-result messages
	if raw.Type != "Results" || len(raw.Channel.Alternatives) == 0 {
		return &Result{}, nil
	}

	return &Result{
		Text:    raw.Channel.Alternatives[0].Transcript,
		IsFinal: raw.IsFinal,
	}, nil
}

func (d *Deepgram) KeepAlive() error {
	msg := []byte(`{"type":"KeepAlive"}`)
	return d.conn.WriteMessage(websocket.TextMessage, msg)
}

func (d *Deepgram) Close() error {
	if d.conn == nil {
		return nil
	}
	d.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"Close"}`))
	return d.conn.Close()
}
