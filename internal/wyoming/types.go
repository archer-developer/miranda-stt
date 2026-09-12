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
type InfoData struct {
	STT []STTInfo `json:"stt"`
}

// STTInfo describes one STT engine exposed by this server.
type STTInfo struct {
	Name      string   `json:"name"`
	Languages []string `json:"languages"`
	Models    []STTModel `json:"models"`
}

// STTModel describes a model variant within an STT engine.
type STTModel struct {
	Name string `json:"name"`
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
