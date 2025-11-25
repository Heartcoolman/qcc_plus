package proxy

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

// WSHub 管理所有 WebSocket 连接。
// 以账号 ID 维度隔离连接集合，确保多租户数据隔离。
type WSHub struct {
	clients map[string]map[*WSClient]bool

	register   chan *WSClient
	unregister chan *WSClient
	broadcast  chan *WSMessage

	mu sync.RWMutex
}

// WSClient 表示一个 WebSocket 客户端连接。
type WSClient struct {
	hub       *WSHub
	conn      *websocket.Conn
	accountID string
	send      chan []byte
	isShare   bool // 是否通过分享链接连接
}

// WSMessage 为 hub 内部广播结构。
type WSMessage struct {
	AccountID string      `json:"account_id"`
	Type      string      `json:"type"` // "node_status", "node_metrics" 等
	Payload   interface{} `json:"payload"`
}

// NewWSHub 创建 hub 实例。
func NewWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[string]map[*WSClient]bool),
		register:   make(chan *WSClient, 10),
		unregister: make(chan *WSClient, 10),
		broadcast:  make(chan *WSMessage, 256),
	}
}

// Run 主循环，串行化注册、注销和广播事件。
func (h *WSHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.addClient(client)
		case client := <-h.unregister:
			h.removeClient(client)
		case message := <-h.broadcast:
			h.broadcastToAccount(message)
		}
	}
}

func (h *WSHub) addClient(client *WSClient) {
	if client == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[client.accountID] == nil {
		h.clients[client.accountID] = make(map[*WSClient]bool)
	}
	h.clients[client.accountID][client] = true
}

func (h *WSHub) removeClient(client *WSClient) {
	if client == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if clients, ok := h.clients[client.accountID]; ok {
		if _, ok := clients[client]; ok {
			delete(clients, client)
			close(client.send)
			if len(clients) == 0 {
				delete(h.clients, client.accountID)
			}
		}
	}
}

func (h *WSHub) broadcastToAccount(message *WSMessage) {
	if message == nil {
		return
	}
	h.mu.RLock()
	clients := h.clients[message.AccountID]
	h.mu.RUnlock()

	if len(clients) == 0 {
		return
	}

	data, err := json.Marshal(message)
	if err != nil {
		return
	}

	for client := range clients {
		select {
		case client.send <- data:
		default:
			// 发送缓冲区已满，主动注销释放资源
			h.unregister <- client
		}
	}
}

// Broadcast 发送消息到指定账号的所有连接。
func (h *WSHub) Broadcast(accountID, msgType string, payload interface{}) {
	if h == nil {
		return
	}
	h.broadcast <- &WSMessage{
		AccountID: accountID,
		Type:      msgType,
		Payload:   payload,
	}
}
