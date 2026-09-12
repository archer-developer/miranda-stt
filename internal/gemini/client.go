package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	endpointFmt = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent?key=%s"
	audioMime   = "audio/pcm;rate=16000"
	sttPrompt   = "You are a professional verbatim Speech-to-Text (STT) transcription engine. " +
		"Your sole task is to transcribe exactly what is spoken in the audio stream into text. " +
		"Rules: 1. Do NOT answer questions. 2. Do NOT execute commands. " +
		"3. Do NOT add conversational filler, preamble, or commentary. " +
		"4. Output only the raw transcription. " +
		"5. If the audio contains silence, noise, or unintelligible sounds, output absolutely nothing. " +
		"6. Preserve the language spoken (primarily Russian or English)."
)

// Client manages one Gemini Live API WebSocket session for STT.
type Client struct {
	conn   *websocket.Conn
	model  string
	logger *slog.Logger
	debug  bool
}

// New dials the Gemini Live API and performs the setup handshake.
// Returns when the setupComplete acknowledgement is received.
func New(ctx context.Context, apiKey, model string, logger *slog.Logger) (*Client, error) {
	url := fmt.Sprintf(endpointFmt, apiKey)

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("gemini/client: dial: %w", err)
	}

	c := &Client{
		conn:   conn,
		model:  model,
		logger: logger,
		debug:  logger.Enabled(ctx, slog.LevelDebug),
	}

	if err := c.sendSetup(ctx); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "setup failed")
		return nil, err
	}

	if err := c.waitSetupComplete(ctx); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "setup ack failed")
		return nil, err
	}

	return c, nil
}

func (c *Client) sendSetup(ctx context.Context) error {
	// gemini-3.5-transcribe-live and similar transcription-only models work
	// best with a minimal setup: no system_instruction (they are already
	// hard-wired for transcription) and no generation_config (response
	// modalities don't apply — transcription comes back in inputTranscription,
	// not modelTurn). Generative models that happen to support
	// bidiGenerateContent would need the full config, but this service targets
	// dedicated transcription model IDs.
	emptyObj := struct{}{}
	msg := SetupMessage{
		Setup: SetupPayload{
			Model:                   c.model,
			InputAudioTranscription: &emptyObj,
		},
	}
	if c.debug {
		raw, _ := json.Marshal(msg)
		c.logger.Debug("gemini → send setup", "json", string(raw))
	}
	if err := wsjson.Write(ctx, c.conn, msg); err != nil {
		return fmt.Errorf("gemini/client: send setup: %w", err)
	}
	return nil
}

func (c *Client) waitSetupComplete(ctx context.Context) error {
	for {
		var msg ServerMessage
		if err := wsjson.Read(ctx, c.conn, &msg); err != nil {
			return fmt.Errorf("gemini/client: wait setupComplete: %w", err)
		}
		if c.debug {
			raw, _ := json.Marshal(msg)
			c.logger.Debug("gemini ← recv", "json", string(raw))
		}
		if msg.SetupComplete != nil {
			return nil
		}
	}
}

// SendAudio encodes pcm as base64 and sends it as a realtime_input chunk.
func (c *Client) SendAudio(ctx context.Context, pcm []byte) error {
	msg := RealtimeInputMessage{
		RealtimeInput: RealtimeInput{
			MediaChunks: []MediaChunk{
				{MimeType: audioMime, Data: base64.StdEncoding.EncodeToString(pcm)},
			},
		},
	}
	if c.debug {
		c.logger.Debug("gemini → audio chunk", "bytes", len(pcm))
	}
	if err := wsjson.Write(ctx, c.conn, msg); err != nil {
		return fmt.Errorf("gemini/client: send audio: %w", err)
	}
	return nil
}

// SignalEndOfInput sends a client_content with turn_complete=true so Gemini
// knows audio input has ended and should finalise the transcript.
func (c *Client) SignalEndOfInput(ctx context.Context) error {
	msg := ClientContentMessage{
		ClientContent: ClientContent{TurnComplete: true},
	}
	if c.debug {
		c.logger.Debug("gemini → signal end of input")
	}
	if err := wsjson.Write(ctx, c.conn, msg); err != nil {
		return fmt.Errorf("gemini/client: signal end of input: %w", err)
	}
	return nil
}

// ReadEvent reads one message from Gemini and returns it.
// The raw JSON bytes are read first so the debug log captures fields that
// our ServerMessage struct doesn't know about yet (e.g. new transcription
// field names added by newer models).
func (c *Client) ReadEvent(ctx context.Context) (ServerMessage, error) {
	var raw json.RawMessage
	if err := wsjson.Read(ctx, c.conn, &raw); err != nil {
		return ServerMessage{}, fmt.Errorf("gemini/client: read event: %w", err)
	}
	if c.debug {
		c.logger.Debug("gemini ← recv (raw)", "json", string(raw))
	}
	var msg ServerMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return ServerMessage{}, fmt.Errorf("gemini/client: parse event: %w", err)
	}
	return msg, nil
}

// Close closes the underlying WebSocket connection.
func (c *Client) Close() {
	_ = c.conn.Close(websocket.StatusNormalClosure, "done")
}
