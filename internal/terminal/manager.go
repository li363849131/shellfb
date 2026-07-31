package terminal

import (
	"errors"
	"sync"
)

var ErrMaxSessions = errors.New("maximum sessions reached")

type SessionInfo struct {
	ID   string
	User string
	Cols uint16
	Rows uint16
}

type Manager struct {
	sessions   map[string]*Session
	userIndex  map[string][]string // username -> []session_id
	maxPerUser int
	mu         sync.RWMutex
}

func NewManager(maxPerUser int) *Manager {
	return &Manager{
		sessions:   make(map[string]*Session),
		userIndex:  make(map[string][]string),
		maxPerUser: maxPerUser,
	}
}

func (m *Manager) Create(username string, cols, rows uint16) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.userIndex[username]) >= m.maxPerUser {
		return nil, ErrMaxSessions
	}

	sess, err := NewSession(cols, rows)
	if err != nil {
		return nil, err
	}

	m.sessions[sess.ID()] = sess
	m.userIndex[username] = append(m.userIndex[username], sess.ID())
	return sess, nil
}

func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	return sess, nil
}

func (m *Manager) ListByUser(username string) []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var infos []SessionInfo
	for _, id := range m.userIndex[username] {
		if sess, ok := m.sessions[id]; ok {
			infos = append(infos, SessionInfo{
				ID:   sess.ID(),
				User: username,
				Cols: sess.Cols(),
				Rows: sess.Rows(),
			})
		}
	}
	return infos
}

func (m *Manager) Destroy(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[id]
	if !ok {
		return
	}
	sess.Close()
	delete(m.sessions, id)

	for user, ids := range m.userIndex {
		for i, sid := range ids {
			if sid == id {
				m.userIndex[user] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
	}
}

func (m *Manager) DestroyAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, sess := range m.sessions {
		sess.Close()
	}
	m.sessions = make(map[string]*Session)
	m.userIndex = make(map[string][]string)
}
