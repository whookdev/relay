package tunnel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/whookdev/relay/internal/models"
)

type Connection struct {
	projectID string
	conn      *websocket.Conn
	logger    *slog.Logger

	pending   map[string]chan *models.Message
	pendingMu sync.RWMutex

	done chan struct{}
}

func NewConnection(projectID string, conn *websocket.Conn) *Connection {
	t := &Connection{
		projectID: projectID,
		conn:      conn,
		logger:    slog.With("component", "tunnel", "project_id", projectID),
		pending:   make(map[string]chan *models.Message),
		done:      make(chan struct{}),
	}

	go t.readPump()

	return t
}

func (t *Connection) Handle() error {
	pingTicker := time.NewTicker(20 * time.Second)
	defer pingTicker.Stop()

	readError := make(chan error, 1)
	go func() {
		readError <- t.readPump()
	}()

	for {
		select {
		case err := <-readError:
			return fmt.Errorf("tunnel closed: %w", err)

		case <-pingTicker.C:
			if err := t.conn.WriteControl(
				websocket.PingMessage,
				[]byte{},
				time.Now().Add(10*time.Second),
			); err != nil {
				return fmt.Errorf("ping failed: %w", err)
			}

		case <-t.done:
			return nil
		}
	}
}

func (t *Connection) ForwardRequest(r *http.Request) (*http.Response, error) {
	requestID := generateRequestID()

	t.logger.Info("attempting to forward request", "request_id", requestID)

	responseChan := make(chan *models.Message, 1)

	t.pendingMu.Lock()
	t.pending[requestID] = responseChan
	t.pendingMu.Unlock()

	defer func() {
		t.pendingMu.Lock()
		delete(t.pending, requestID)
		t.pendingMu.Unlock()
	}()

	msg := &models.Message{
		Type:      "request",
		RequestID: requestID,
		Method:    r.Method,
		Path:      r.URL.Path,
		Headers:   r.Header,
	}

	// TODO: Body is always non-nil
	if r.Body != nil {
		// TODO: Consider streams
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("reading request body: %w", err)
		}
		msg.Body = json.RawMessage(body)
	}

	t.logger.Info("attempting to send message", "message", msg)

	if err := t.conn.WriteJSON(msg); err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	select {
	case resp := <-responseChan:
		return createHTTPResponse(resp)
	case <-time.After(30 * time.Second):
		t.logger.Error("timeout waiting for response")
		return createHTTPResponse(msg)
	case <-t.done:
		return nil, fmt.Errorf("tunnel closed")
	}
}

func (t *Connection) readPump() error {
	defer func() {
		t.logger.Info("readPump ending", "project_id", t.projectID)
		t.conn.Close()
		close(t.done)
	}()

	t.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	t.conn.SetPongHandler(func(string) error {
		t.logger.Debug("received pong")
		return t.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	for {
		var msg models.Message
		err := t.conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure) {
				t.logger.Error("websocket read error", "error", err)
				return fmt.Errorf("websocket read error: %w", err)
			}
			t.logger.Info("websocket closed normally")
			return nil
		}

		t.logger.Info("received message",
			"type", msg.Type,
			"request_id", msg.RequestID)

		switch msg.Type {
		case "response":
			t.handleResponse(&msg)
		case "error":
			t.logger.Error("received error from client",
				"request_id", msg.RequestID,
				"error", string(msg.Body))
		}
	}
}

func (t *Connection) handleResponse(msg *models.Message) {
	t.pendingMu.RLock()
	ch, exists := t.pending[msg.RequestID]
	t.pendingMu.RUnlock()

	if exists {
		select {
		case ch <- msg:
			// Succesful response, do nothing?
		default:
			t.logger.Warn("dropped response - channel full",
				"request_id", msg.RequestID)
		}
	} else {
		t.logger.Warn("received response for unknown request",
			"request_id", msg.RequestID)
	}
}

func createHTTPResponse(msg *models.Message) (*http.Response, error) {
	resp := &http.Response{
		StatusCode: msg.StatusCode,
		Header:     msg.Headers,
		Body:       io.NopCloser(bytes.NewReader(msg.Body)),
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
	return resp, nil
}

func generateRequestID() string {
	return fmt.Sprintf("req_%s", uuid.New().String())
}
