// Package gemini implements the client side of the Google Gemini Multimodal
// Live API (BidiGenerateContent WebSocket protocol), configured strictly for
// STT transcription.
package gemini

// SetupMessage is the first message the client sends after opening the
// WebSocket to configure the Gemini session.
type SetupMessage struct {
	Setup SetupPayload `json:"setup"`
}

// SetupPayload carries session-level configuration.
type SetupPayload struct {
	Model                 string             `json:"model"`
	GenerationConfig      *GenerationConfig  `json:"generation_config,omitempty"`
	SystemInstruction     *SystemInstruction `json:"system_instruction,omitempty"`
	// InputAudioTranscription, when set, enables transcription of the
	// user's input audio. Used by transcription-specific models such as
	// gemini-3.5-transcribe-live.
	InputAudioTranscription  *struct{} `json:"input_audio_transcription,omitempty"`
	// OutputAudioTranscription enables transcription of the model's output.
	OutputAudioTranscription *struct{} `json:"output_audio_transcription,omitempty"`
}

// GenerationConfig restricts Gemini to text-only output at zero temperature.
type GenerationConfig struct {
	ResponseModalities []string `json:"response_modalities"`
	Temperature        float64  `json:"temperature"`
}

// SystemInstruction injects the STT-only system prompt.
type SystemInstruction struct {
	Parts []Part `json:"parts"`
}

// Part holds a single text segment of a content message.
type Part struct {
	Text string `json:"text,omitempty"`
}

// RealtimeInputMessage wraps one or more audio chunks sent to Gemini.
type RealtimeInputMessage struct {
	RealtimeInput RealtimeInput `json:"realtime_input"`
}

// RealtimeInput holds the list of media chunks in a single send.
type RealtimeInput struct {
	MediaChunks []MediaChunk `json:"media_chunks"`
}

// MediaChunk carries a Base64-encoded PCM audio blob with its MIME type.
type MediaChunk struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64-encoded
}

// ClientContentMessage signals the end of input to Gemini.
type ClientContentMessage struct {
	ClientContent ClientContent `json:"client_content"`
}

// ClientContent with TurnComplete=true tells Gemini the turn is over.
type ClientContent struct {
	TurnComplete bool `json:"turn_complete"`
}

// ServerContent carries incremental transcription deltas and completion flags.
// Fields vary by model family:
//   - Generative models: text deltas in ModelTurn.Parts
//   - Transcription models (gemini-3.5-transcribe-live):
//       interimInputTranscription — rolling best-guess, replaced by each event
//       finalInputTranscription   — final stable result for this speech segment
type ServerContent struct {
	ModelTurn                *ModelTurn     `json:"modelTurn,omitempty"`
	TurnComplete             bool           `json:"turnComplete,omitempty"`
	InterimInputTranscription *Transcription `json:"interimInputTranscription,omitempty"`
	FinalInputTranscription   *Transcription `json:"finalInputTranscription,omitempty"`
	InputTranscription        *Transcription `json:"inputTranscription,omitempty"`
	OutputTranscription       *Transcription `json:"outputTranscription,omitempty"`
}

// ModelTurn holds the text parts produced in this delta (generative models).
type ModelTurn struct {
	Parts []Part `json:"parts"`
}

// Transcription holds a text result from a transcription-specific model.
type Transcription struct {
	Text string `json:"text"`
}

// VoiceActivity signals when Gemini detects speech start or end.
// Used with transcription models that have automatic activity detection.
type VoiceActivity struct {
	Type        string `json:"type"`        // "ACTIVITY_START" or "ACTIVITY_END"
	AudioOffset string `json:"audioOffset"` // e.g. "0.160s"
}

// ServerMessage is the envelope for all messages Gemini sends back.
type ServerMessage struct {
	SetupComplete *struct{}      `json:"setupComplete,omitempty"`
	ServerContent *ServerContent `json:"serverContent,omitempty"`
	VoiceActivity *VoiceActivity `json:"voiceActivity,omitempty"`
}
