package server

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*websocket.Conn]bool),
	}
}

func (h *Hub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = true
}

func (h *Hub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
	conn.Close()
}

func (h *Hub) Broadcast(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.RLock()
	var dead []*websocket.Conn
	for conn := range h.clients {
		conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			dead = append(dead, conn)
		}
	}
	h.mu.RUnlock()
	for _, conn := range dead {
		h.Unregister(conn)
	}
}

type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type ProgressPayload struct {
	JobID       string  `json:"job_id"`
	TrackID     int     `json:"track_id"`
	TrackTitle  string  `json:"track_title"`
	TrackArtist string  `json:"track_artist"`
	Status      string  `json:"status"` // queued, downloading, tagging, done, error
	Progress    float64 `json:"progress"`
	Message     string  `json:"message,omitempty"`
}

type JobPayload struct {
	JobID     string         `json:"job_id"`
	URL       string         `json:"url"`
	Name      string         `json:"name"`      // resolved playlist/release/artist name
	Status    string         `json:"status"`    // pending, running, done, error
	Total     int            `json:"total"`
	Completed int            `json:"completed"`
	Failed    int            `json:"failed"`
	Tracks    []TrackSummary `json:"tracks,omitempty"`
	Message   string         `json:"message,omitempty"`
	HasFiles  bool           `json:"has_files"`
}

type TrackSummary struct {
	ID      int    `json:"id"`
	Artist  string `json:"artist"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}
