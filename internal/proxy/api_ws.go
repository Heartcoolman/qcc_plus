package proxy

import (
	"errors"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// TODO: 生产环境校验 Origin 防止 CSRF
		return true
	},
}

// GET /api/monitor/ws?token=xxx
func (p *Server) handleMonitorWebSocket(w http.ResponseWriter, r *http.Request) {
	if p == nil || p.wsHub == nil {
		http.Error(w, "websocket not available", http.StatusServiceUnavailable)
		return
	}

	accountID, err := p.authenticateWSRequest(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		p.logger.Printf("websocket upgrade failed: %v", err)
		return
	}

	client := &WSClient{
		hub:       p.wsHub,
		conn:      conn,
		accountID: accountID,
		send:      make(chan []byte, 256),
	}
	p.wsHub.register <- client

	go client.writePump()
	go client.readPump()
}

// authenticateWSRequest 支持 session cookie 或分享 token。
func (p *Server) authenticateWSRequest(r *http.Request) (string, error) {
	// Session cookie
	if sess := getSessionFromCookie(p.sessionMgr, r); sess != nil {
		return sess.AccountID, nil
	}

	// Share token
	shareToken := r.URL.Query().Get("token")
	if shareToken != "" {
		if p.store == nil {
			return "", errors.New("share token not supported")
		}
		share, err := p.store.GetMonitorShareByToken(r.Context(), shareToken)
		if err != nil || share == nil {
			return "", errors.New("invalid share token")
		}
		return share.AccountID, nil
	}

	return "", errors.New("authentication required")
}

// getSessionFromCookie 返回有效会话。
func getSessionFromCookie(mgr *SessionManager, r *http.Request) *Session {
	if mgr == nil || r == nil {
		return nil
	}
	cookie, err := r.Cookie("session_token")
	if err != nil || cookie == nil || cookie.Value == "" {
		return nil
	}
	return mgr.Get(cookie.Value)
}
