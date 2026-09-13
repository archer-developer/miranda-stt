package wyoming

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/archer-developer/miranda-stt/internal/audio"
	"github.com/archer-developer/miranda-stt/internal/gemini"
)

// triggerReason records which side ended the STT turn.
type triggerReason string

const (
	triggerHAAudioStop      triggerReason = "ha_audio_stop"
	triggerGeminiTurnComplete triggerReason = "gemini_turn_complete"
)

// Session handles one Wyoming client connection from start to finish.
type Session struct {
	id           string
	conn         net.Conn
	reader       *Reader
	writer       *Writer
	logger       *slog.Logger
	apiKey       string
	model        string
	languages    []string
	audioDumpDir string
	turnTimeout  time.Duration
}

// NewSession constructs a Session for an accepted TCP connection.
func NewSession(
	id string,
	conn net.Conn,
	logger *slog.Logger,
	apiKey, model string,
	languages []string,
	audioDumpDir string,
	turnTimeout time.Duration,
) *Session {
	return &Session{
		id:           id,
		conn:         conn,
		reader:       NewReader(conn),
		writer:       NewWriter(conn),
		logger:       logger.With("session_id", id),
		apiKey:       apiKey,
		model:        model,
		languages:    languages,
		audioDumpDir: audioDumpDir,
		turnTimeout:  turnTimeout,
	}
}

// Run processes the full Wyoming session lifecycle until the connection closes.
func (s *Session) Run(ctx context.Context) {
	defer func() { _ = s.conn.Close() }()

	for {
		if err := s.runOnce(ctx); err != nil {
			if isEOF(err) {
				s.logger.Info("client disconnected")
				return
			}
			s.logger.Error("session error", "error", err)
			return
		}
	}
}

// runOnce handles one describe→transcribe→audio→transcript cycle.
func (s *Session) runOnce(ctx context.Context) error {
	h, err := s.reader.ReadHeader()
	if err != nil {
		return err
	}
	s.logger.Debug("wyoming ← recv", "type", h.Type)

	switch h.Type {
	case "describe":
		return s.handleDescribe()
	case "transcribe":
		return s.handleTranscribe(ctx, h)
	default:
		s.logger.Warn("unexpected message type, ignoring", "type", h.Type)
		return nil
	}
}

func (s *Session) handleDescribe() error {
	// Strip the "models/" prefix for the advertised model name.
	modelName := strings.TrimPrefix(s.model, "models/")
	attr := Attribution{Name: "Google", URL: "https://ai.google.dev"}

	info := InfoData{
		ASR: []ASRProgram{
			{
				Name:        "gemini-live-stt",
				Description: "Gemini Live STT via Multimodal Live API",
				Attribution: attr,
				Installed:   true,
				Languages:   s.languages,
				Models: []ASRModel{
					{
						Name:        modelName,
						Description: "Gemini transcription model",
						Attribution: attr,
						Installed:   true,
						Languages:   s.languages,
					},
				},
			},
		},
	}
	s.logger.Debug("wyoming → send info")
	return s.writer.WriteMessage("info", info, nil)
}

func (s *Session) handleTranscribe(ctx context.Context, _ Header) error {
	// Read audio-start
	h, err := s.reader.ReadHeader()
	if err != nil {
		return fmt.Errorf("session: waiting for audio-start: %w", err)
	}
	if h.Type != "audio-start" {
		return fmt.Errorf("session: expected audio-start, got %q", h.Type)
	}
	s.logger.Debug("wyoming ← audio-start")

	// Establish Gemini session
	gClient, err := gemini.New(ctx, s.apiKey, s.model, s.logger)
	if err != nil {
		return fmt.Errorf("session: connect gemini: %w", err)
	}
	defer gClient.Close()

	var wavBuf *audio.Buffer
	if s.audioDumpDir != "" {
		wavBuf = &audio.Buffer{}
	}

	// Channels to receive signals from the goroutine reading Gemini events.
	type geminiResult struct {
		text   string
		err    error
		reason triggerReason
	}
	geminiCh := make(chan geminiResult, 1)

	// audioStopCh is closed when HA sends audio-stop.
	audioStopCh := make(chan struct{})

	// latestInterimPtr lets the audio-stop timeout path fall back to the last
	// interim transcription when Gemini never fires ACTIVITY_END or
	// finalInputTranscription within the timeout. Atomic pointer avoids a mutex
	// for what is effectively a write-once-many-times value.
	var latestInterimPtr atomic.Pointer[string]

	startedAt := time.Now()
	var audioDurationMs int64

	// Goroutine: stream audio from Wyoming to Gemini and detect audio-stop.
	go func() {
		for {
			ah, err := s.reader.ReadHeader()
			if err != nil {
				// Connection closed or error — treat as audio-stop.
				close(audioStopCh)
				return
			}
			s.logger.Debug("wyoming ← recv", "type", ah.Type)

			switch ah.Type {
			case "audio-chunk":
				payload, err := s.reader.ReadPayload(ah.PayloadLength)
				if err != nil {
					s.logger.Error("read audio payload", "error", err)
					close(audioStopCh)
					return
				}
				audioDurationMs += int64(ah.PayloadLength) * 1000 / (16000 * 2) // 16-bit mono
				if wavBuf != nil {
					wavBuf.Write(payload)
				}
				if err := gClient.SendAudio(ctx, payload); err != nil {
					s.logger.Error("send audio to gemini", "error", err)
					close(audioStopCh)
					return
				}

			case "audio-stop":
				s.logger.Debug("wyoming ← audio-stop")
				close(audioStopCh)
				return
			}
		}
	}()

	// Goroutine: read Gemini events and produce the final transcript.
	//
	// Model families:
	//   Generative models: accumulate text deltas from serverContent.modelTurn,
	//   complete on serverContent.turnComplete.
	//   Transcription models (gemini-3.5-transcribe-live): receive rolling
	//   interimInputTranscription events (each supersedes the last), then either:
	//     a) finalInputTranscription arrives → definitive result, OR
	//     b) voiceActivity.type == "ACTIVITY_END" → latest interim is the result.
	go func() {
		var deltasBuf strings.Builder // generative model: delta accumulator
		var latestInterim string       // transcription model: latest rolling result
		for {
			msg, err := gClient.ReadEvent(ctx)
			if err != nil {
				geminiCh <- geminiResult{err: fmt.Errorf("session: read gemini event: %w", err)}
				return
			}

			// ACTIVITY_END from voice-activity detection means Gemini detected
			// end of speech. The latest interim transcription is the final result.
			if msg.VoiceActivity != nil && msg.VoiceActivity.Type == "ACTIVITY_END" {
				s.logger.Debug("gemini voice activity end", "latest_interim", latestInterim)
				geminiCh <- geminiResult{text: latestInterim, reason: triggerGeminiTurnComplete}
				return
			}

			if msg.ServerContent == nil {
				continue
			}
			sc := msg.ServerContent

			// Generative model: accumulate text deltas
			if sc.ModelTurn != nil {
				for _, p := range sc.ModelTurn.Parts {
					deltasBuf.WriteString(p.Text)
				}
			}
			if sc.InputTranscription != nil && sc.InputTranscription.Text != "" {
				deltasBuf.WriteString(sc.InputTranscription.Text)
			}

			// Transcription model: rolling interim updates (each replaces the last)
			if sc.InterimInputTranscription != nil && sc.InterimInputTranscription.Text != "" {
				latestInterim = sc.InterimInputTranscription.Text
				latestInterimPtr.Store(&latestInterim)
				s.logger.Debug("gemini interim", "text", latestInterim)
			}

			// Transcription model: final stable result for this speech segment
			if sc.FinalInputTranscription != nil {
				s.logger.Debug("gemini final transcription", "text", sc.FinalInputTranscription.Text)
				geminiCh <- geminiResult{text: sc.FinalInputTranscription.Text, reason: triggerGeminiTurnComplete}
				return
			}

			// Generative model: turn is done
			if sc.TurnComplete {
				text := deltasBuf.String()
				if text == "" {
					text = latestInterim
				}
				geminiCh <- geminiResult{text: text, reason: triggerGeminiTurnComplete}
				return
			}
		}
	}()

	// Wait for whichever trigger fires first.
	var transcript string
	var reason triggerReason

	select {
	case <-audioStopCh:
		// HA signalled end — tell Gemini, then wait for final turn_complete with timeout.
		if err := gClient.SignalEndOfInput(ctx); err != nil {
			s.logger.Warn("signal end of input failed", "error", err)
		}
		timeoutCtx, cancel := context.WithTimeout(ctx, s.turnTimeout)
		defer cancel()
		select {
		case res := <-geminiCh:
			if res.err != nil {
				s.logger.Warn("gemini error after audio-stop", "error", res.err)
			}
			transcript = res.text
		case <-timeoutCtx.Done():
			// Gemini never fired ACTIVITY_END / finalInputTranscription within
			// the timeout. Fall back to the last interim transcription the
			// goroutine received — for transcription models this is the best
			// available result.
			if p := latestInterimPtr.Load(); p != nil {
				transcript = *p
			}
			s.logger.Warn("gemini timeout, using latest interim", "text", transcript)
		}
		reason = triggerHAAudioStop

	case res := <-geminiCh:
		if res.err != nil {
			return fmt.Errorf("session: gemini: %w", res.err)
		}
		transcript = res.text
		reason = res.reason
	}

	processingMs := time.Since(startedAt).Milliseconds()

	s.logger.Info("transcription complete",
		"audio_ms", audioDurationMs,
		"processing_ms", processingMs,
		"text", transcript,
		"trigger", reason,
	)

	// Save WAV dump if enabled.
	if wavBuf != nil && wavBuf.Len() > 0 {
		ts := time.Now().Format("20060102_150405")
		fname := fmt.Sprintf("%s_%s_%s.wav", ts, s.id, reason)
		path := filepath.Join(s.audioDumpDir, fname)
		if err := wavBuf.SaveWAV(path); err != nil {
			s.logger.Error("save wav dump", "error", err, "path", path)
		} else {
			s.logger.Info("audio dump saved", "path", path)
		}
	}

	s.logger.Debug("wyoming → send transcript", "text", transcript)
	return s.writer.WriteMessage("transcript", TranscriptData{Text: transcript}, nil)
}

// isEOF reports whether err represents a clean client disconnect.
func isEOF(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "use of closed network connection") ||
		os.IsTimeout(err)
}
