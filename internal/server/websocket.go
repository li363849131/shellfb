package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/valorisa/ShellFromBrowser/internal/ssh"
	"github.com/valorisa/ShellFromBrowser/internal/terminal"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
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
			conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"session not found"}`))
			return
		}
	} else {
		sess, err = s.sessions.Create(username, 80, 24)
		if err != nil {
			conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"`+err.Error()+`"}`))
			return
		}
	}

	// Send session info
	info, _ := json.Marshal(map[string]string{"type": "session", "id": sess.ID()})
	conn.WriteMessage(websocket.TextMessage, info)

	// Register client with session broadcaster and get history snapshot
	ch, history := sess.RegisterClient()
	defer sess.UnregisterClient(ch)

	// Send history snapshot immediately so new client gets current screen & prompt
	if len(history) > 0 {
		if err := conn.WriteMessage(websocket.BinaryMessage, history); err != nil {
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
			if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
				return
			}
		}
	}()

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

	target := r.URL.Query().Get("target")
	password := r.URL.Query().Get("password")
	keyFile := r.URL.Query().Get("key")

	if target == "" {
		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"target required"}`))
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
		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"`+err.Error()+`"}`))
		return
	}
	defer sshSess.Close()

	// SSH stdout -> WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := sshSess.Read(buf)
			if err != nil {
				return
			}
			conn.WriteMessage(websocket.BinaryMessage, buf[:n])
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
