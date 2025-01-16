package models

import "net/http"

type Message struct {
	Type      string      `json:"type"`
	RequestID string      `json:"request_id"`
	Method    string      `json:"method,omitempty"`
	Path      string      `json:"path,omitempty"`
	Headers   http.Header `json:"headers,omitempty"`
	Body      []byte      `json:"body,omitempty"`

	// For responses
	StatusCode int `json:"status_code,omitempty"`
}
