// Package wyoming implements the Wyoming speech protocol as used by
// Home Assistant for STT integration.
package wyoming

// Header is the JSON envelope that precedes every Wyoming message on the wire.
// If PayloadLength > 0, exactly that many bytes of binary payload follow the
// newline that terminates the header.
type Header struct {
	Type          string      `json:"type"`
	Data          interface{} `json:"data,omitempty"`
	PayloadLength int         `json:"payload_length,omitempty"`
}

// InfoData is the response payload for the "describe" request.
// The field name must be "asr" per the Wyoming protocol spec.
type InfoData struct {
	ASR []ASRProgram `json:"asr"`
}

// Attribution holds the name and URL of who provides the service.
type Attribution struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ASRProgram describes one STT engine exposed by this server.
type ASRProgram struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Languages   []string    `json:"languages"`
	Models      []ASRModel  `json:"models"`
}

// ASRModel describes a model variant within an ASR program.
type ASRModel struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Languages   []string    `json:"languages"`
}

// TranscribeData is the payload of the "transcribe" event from the client.
type TranscribeData struct {
	Name     string `json:"name"`
	Language string `json:"language"`
}

// AudioStartData is the payload of the "audio-start" event.
type AudioStartData struct {
	Rate     int `json:"rate"`
	Width    int `json:"width"`
	Channels int `json:"channels"`
}

// AudioChunkData is the header payload of an "audio-chunk" event.
// The actual PCM bytes follow as a binary payload of length PayloadLength.
type AudioChunkData struct {
	Rate     int `json:"rate"`
	Width    int `json:"width"`
	Channels int `json:"channels"`
}

// TranscriptData is the payload sent back to the client with the recognised text.
type TranscriptData struct {
	Text string `json:"text"`
}
