package goserver

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
	sendBufferSize = 256
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type WSBroker interface {
	Publish(topic string, message []byte) error
	Subscribe(topic string, handler func(message []byte)) error
	Close()
}

type wsEnvelope struct {
	ChannelName string `json:"c"`
	Sid         string `json:"sid"`
	Payload     []byte `json:"p"`
}

type Channel struct {
	subscribers map[*Client]bool
	name        string
	mu          sync.RWMutex
}

type WebSocketHub struct {
	broker      WSBroker
	channels    map[string]*Channel
	brokerTopic string
	serverID    string
	mu          sync.RWMutex
}

type Client struct {
	hub     *WebSocketHub
	conn    *websocket.Conn
	send    chan []byte
	channel *Channel
}

func NewWebSocketHub() *WebSocketHub {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	srvID := fmt.Sprintf("%x", b)

	return &WebSocketHub{
		channels: make(map[string]*Channel),
		serverID: srvID,
	}
}

func (h *WebSocketHub) SetBroker(broker WSBroker, topicName string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.broker = broker
	h.brokerTopic = topicName

	err := h.broker.Subscribe(h.brokerTopic, func(packedMsg []byte) {
		var envelope wsEnvelope
		if err := json.Unmarshal(packedMsg, &envelope); err != nil {
			log.Printf("WS Broker unmarshal error: %v", err)
			return
		}

		if envelope.Sid == h.serverID {
			return
		}

		h.broadcastLocalOnly(envelope.ChannelName, envelope.Payload)
	})

	return err
}

func (h *WebSocketHub) getOrCreateChannel(name string) *Channel {
	h.mu.RLock()
	ch, ok := h.channels[name]
	h.mu.RUnlock()
	if ok {
		return ch
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok = h.channels[name]; ok {
		return ch
	}
	ch = &Channel{
		name:        name,
		subscribers: make(map[*Client]bool),
	}
	h.channels[name] = ch
	return ch
}

func (h *WebSocketHub) Register(client *Client, channelName string) {
	ch := h.getOrCreateChannel(channelName)
	client.channel = ch
	ch.mu.Lock()
	ch.subscribers[client] = true
	ch.mu.Unlock()
}

func (h *WebSocketHub) Unregister(client *Client) {
	if client.channel == nil {
		return
	}
	ch := client.channel
	ch.mu.Lock()
	delete(ch.subscribers, client)
	empty := len(ch.subscribers) == 0
	ch.mu.Unlock()

	if empty {
		h.mu.Lock()
		defer h.mu.Unlock()
		ch.mu.Lock()
		if len(ch.subscribers) == 0 {
			delete(h.channels, ch.name)
		}
		ch.mu.Unlock()
	}
	close(client.send)
}

func (h *WebSocketHub) Broadcast(channelName string, message []byte) {
	h.broadcastLocalOnly(channelName, message)

	if h.broker != nil {
		envelope := wsEnvelope{
			ChannelName: channelName,
			Payload:     message,
			Sid:         h.serverID,
		}

		packed, err := json.Marshal(envelope)
		if err == nil {
			go func() {
				_ = h.broker.Publish(h.brokerTopic, packed)
			}()
		} else {
			log.Printf("WS Broker marshal error: %v", err)
		}
	}
}

func (h *WebSocketHub) broadcastLocalOnly(channelName string, message []byte) {
	h.mu.RLock()
	ch, ok := h.channels[channelName]
	h.mu.RUnlock()

	if !ok {
		return
	}

	ch.mu.RLock()
	defer ch.mu.RUnlock()

	for client := range ch.subscribers {
		select {
		case client.send <- message:
		default:

		}
	}
}

func (h *WebSocketHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS Upgrade error: %v", err)
		return
	}

	client := &Client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, sendBufferSize),
	}

	go client.writePump()

	client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	_, msg, err := c.conn.ReadMessage()
	if err != nil {
		if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
			log.Printf("WS Error (First msg): %v", err)
		}
		return
	}
	channelName := string(msg)
	c.hub.Register(c, channelName)

	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WS Error: %v", err)
			}
			break
		}
		c.hub.Broadcast(channelName, msg)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
