// Package wyoming implements the Wyoming speech protocol as used by
// Home Assistant for STT integration.
package wyoming

// Header is the JSON envelope that precedes every Wyoming message on the wire.
//
// Wyoming protocol 1.9+ separates event data from the header:
//   - DataLength > 0: read exactly that many bytes after the \n as event data JSON.
//   - PayloadLength > 0: read that many bytes of binary payload after the data.
//
// Older clients inline event data in the "data" field. Both formats are
// supported on read; we always write in the 1.9 format.
type Header struct {
	Type          string      `json:"type"`
	Version       string      `json:"version,omitempty"`
	Data          interface{} `json:"data,omitempty"`          // legacy inline format
	DataLength    int         `json:"data_length,omitempty"`   // Wyoming 1.9+
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
