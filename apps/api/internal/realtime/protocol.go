package realtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"unicode/utf8"
)

const inboundLimit = 16 * 1024
const responseLimit = 256 * 1024
const outboundQueue = 64

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var cursorPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var errEnvelope = errors.New("invalid envelope")

type envelope struct {
	V         int             `json:"v"`
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

func parseCommand(data []byte) (envelope, error) {
	if !utf8.Valid(data) || len(data) > inboundLimit {
		return envelope{}, errEnvelope
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var value envelope
	if err := dec.Decode(&value); err != nil || dec.Decode(new(any)) != io.EOF || value.V != 1 ||
		value.Type == "" || !requestIDPattern.MatchString(value.RequestID) || len(value.Payload) == 0 {
		return envelope{}, errEnvelope
	}
	return value, nil
}

func payloadInto(raw json.RawMessage, target any) error {
	if len(raw) == 0 || raw[0] != '{' {
		return errEnvelope
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return errEnvelope
	}
	if dec.Decode(new(any)) != io.EOF {
		return errEnvelope
	}
	return nil
}

func wire(typ, requestID string, payload any) []byte {
	data, _ := json.Marshal(struct {
		V         int    `json:"v"`
		Type      string `json:"type"`
		RequestID string `json:"requestId,omitempty"`
		Payload   any    `json:"payload"`
	}{1, typ, requestID, payload})
	return data
}

type protocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
