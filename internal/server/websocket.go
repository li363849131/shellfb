package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/li363849131/shellfb/internal/ssh"
	"github.com/li363849131/shellfb/internal/terminal"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const (
	// Proxies (Cloudflare tunnels in particular) drop connections that carry no
	// traffic for ~100s. An idle terminal produces nothing, so without a
	// heartbeat the socket dies silently: the browser keeps a CLOSING/CLOSED
	// socket that swallows keystrokes and only a page reload recovers it.
	pingInterval = 25 * time.Second
	pongWait     = 70 * time.Second
	writeWait    = 10 * time.Second
)

// connWriter serialises all writes to a websocket. gorilla/websocket permits
// only one concurrent writer, and here the PTY pump, the ping ticker and the
// error paths all write.
type connWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *connWriter) write(msgType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return w.conn.WriteMessage(msgType, data)
}

// keepalive pings until done closes, so a dead peer is detected within pongWait
// instead of hanging forever.
func keepalive(w *connWriter, done <-chan struct{}) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := w.write(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump configures deadline handling so ReadMessage unblocks when the peer
// goes away without a close frame.
func readPump(conn *websocket.Conn) {
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
}

type resizeMsg struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade: %v", err)
		return
	}
	defer conn.Close()

	w2 := &connWriter{conn: conn}
	readPump(conn)

	username := "anonymous"
	if s.authProv != nil {
		token := r.URL.Query().Get("token")
		claims, _ := s.authProv.ValidateToken(token)
		if claims != nil {
			username = claims.Username
		}
	}

	sessionID := r.URL.Query().Get("session")
	var sess *terminal.Session

	if sessionID != "" {
		sess, err = s.sessions.Get(sessionID)
		if err != nil {
			// The session was destroyed (possibly from another browser tab).
			// Tell the client explicitly so it can drop its stale tab instead of
			// silently reconnecting forever.
			w2.write(websocket.TextMessage, []byte(`{"type":"error","code":"session_gone","message":"session not found"}`))
			return
		}
	} else {
		sess, err = s.sessions.Create(username, 80, 24)
		if err != nil {
			w2.write(websocket.TextMessage, []byte(`{"type":"error","message":"`+err.Error()+`"}`))
			return
		}
	}

	// Send session info
	info, _ := json.Marshal(map[string]string{"type": "session", "id": sess.ID()})
	w2.write(websocket.TextMessage, info)

	// Register client with session broadcaster and get history snapshot
	ch, history := sess.RegisterClient()
	defer sess.UnregisterClient(ch)

	// Send history snapshot immediately so new client gets current screen & prompt
	if len(history) > 0 {
		if err := w2.write(websocket.BinaryMessage, history); err != nil {
			return
		}
	}

	// Session broadcast channel -> WebSocket
	done := make(chan struct{})
	go func() {
		defer close(done)
		for data := range ch {
			if data == nil {
				return
			}
			if err := w2.write(websocket.BinaryMessage, data); err != nil {
				return
			}
		}
	}()

	go keepalive(w2, done)

	// WebSocket -> PTY
	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if msgType == websocket.TextMessage {
			var wsMsg resizeMsg
			if json.Unmarshal(msg, &wsMsg) == nil && wsMsg.Type == "resize" {
				sess.Resize(wsMsg.Cols, wsMsg.Rows)
				continue
			}
		}
		sess.Write(msg)
	}
}

func (s *Server) handleSSHWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	w2 := &connWriter{conn: conn}
	readPump(conn)

	target := r.URL.Query().Get("target")
	password := r.URL.Query().Get("password")
	keyFile := r.URL.Query().Get("key")

	if target == "" {
		w2.write(websocket.TextMessage, []byte(`{"type":"error","message":"target required"}`))
		return
	}

	sshSess, err := ssh.Connect(ssh.ConnectOpts{
		Target:   target,
		Password: password,
		KeyFile:  keyFile,
		Cols:     80,
		Rows:     24,
	})
	if err != nil {
		w2.write(websocket.TextMessage, []byte(`{"type":"error","message":"`+err.Error()+`"}`))
		return
	}
	defer sshSess.Close()

	done := make(chan struct{})
	defer close(done)
	go keepalive(w2, done)

	// SSH stdout -> WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := sshSess.Read(buf)
			if err != nil {
				return
			}
			if err := w2.write(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// WebSocket -> SSH stdin
	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.TextMessage {
			var wsMsg resizeMsg
			if json.Unmarshal(msg, &wsMsg) == nil && wsMsg.Type == "resize" {
				sshSess.Resize(uint32(wsMsg.Cols), uint32(wsMsg.Rows))
				continue
			}
		}
		sshSess.Write(msg)
	}
}
