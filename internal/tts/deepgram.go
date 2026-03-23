package tts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const deepgramTTSURL = "wss://api.deepgram.com/v1/speak"

type DeepgramTTS struct {
	conn   *websocket.Conn
	apiKey string
	model  string
}

type ttsMessage struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ttsResponse struct {
	Type string `json:"type"`
}

func NewDeepgramTTS(apiKey, model string) *DeepgramTTS {
	return &DeepgramTTS{
		apiKey: apiKey,
		model:  model,
	}
}

func (d *DeepgramTTS) Connect(ctx context.Context) error {
	url := fmt.Sprintf("%s?model=%s&encoding=linear16&sample_rate=24000", deepgramTTSURL, d.model)

	header := http.Header{}
	header.Set("Authorization", "Token "+d.apiKey)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		return fmt.Errorf("deepgram tts connect: %w", err)
	}
	d.conn = conn
	return nil
}

func (d *DeepgramTTS) SendText(text string) error {
	msg, err := json.Marshal(ttsMessage{Type: "Speak", Text: text})
	if err != nil {
		return err
	}
	return d.conn.WriteMessage(websocket.TextMessage, msg)
}

func (d *DeepgramTTS) Flush() error {
	msg, err := json.Marshal(ttsMessage{Type: "Flush"})
	if err != nil {
		return err
	}
	return d.conn.WriteMessage(websocket.TextMessage, msg)
}

// Receive reads the next message from Deepgram TTS.
// Returns audio bytes for binary frames.
// Returns ErrFlushed on any Flushed message; the caller decides
// whether this represents the final flush or an intermediate one.
func (d *DeepgramTTS) Receive() ([]byte, error) {
	for {
		msgType, data, err := d.conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("deepgram tts read: %w", err)
		}

		if msgType == websocket.BinaryMessage {
			return data, nil
		}

		if msgType == websocket.TextMessage {
			var resp ttsResponse
			if json.Unmarshal(data, &resp) == nil && resp.Type == "Flushed" {
				return nil, ErrFlushed
			}
		}
	}
}

func (d *DeepgramTTS) Close() error {
	if d.conn == nil {
		return nil
	}
	msg, _ := json.Marshal(ttsMessage{Type: "Close"})
	d.conn.WriteMessage(websocket.TextMessage, msg)
	return d.conn.Close()
}
