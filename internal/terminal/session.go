package terminal

import (
	"io"
	"sync"
)

type Session struct {
	id      string
	reader  io.Reader
	writer  io.Writer
	closer  io.Closer
	cols    uint16
	rows    uint16
	mu      sync.Mutex
	closed  bool
	resizer Resizer

	// Multi-client broadcaster & history replay
	clients    map[chan []byte]bool
	history    []byte
	maxHistory int
	readOnce   sync.Once
}

type Resizer interface {
	Resize(cols, rows uint16) error
}

func (s *Session) ID() string {
	return s.id
}

func (s *Session) initBroadcaster() {
	if s.clients == nil {
		s.clients = make(map[chan []byte]bool)
		s.maxHistory = 200000 // 200KB rolling history buffer
	}
}

func (s *Session) startReading() {
	s.readOnce.Do(func() {
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := s.reader.Read(buf)
				if err != nil {
					s.Close()
					return
				}
				data := make([]byte, n)
				copy(data, buf[:n])

				s.mu.Lock()
				s.history = append(s.history, data...)
				if len(s.history) > s.maxHistory {
					s.history = s.history[len(s.history)-s.maxHistory:]
				}

				for ch := range s.clients {
					select {
					case ch <- data:
					default:
					}
				}
				s.mu.Unlock()
			}
		}()
	})
}

func (s *Session) RegisterClient() (chan []byte, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.initBroadcaster()

	ch := make(chan []byte, 256)
	s.clients[ch] = true

	histCopy := make([]byte, len(s.history))
	copy(histCopy, s.history)

	s.startReading()

	return ch, histCopy
}

func (s *Session) UnregisterClient(ch chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.clients != nil {
		delete(s.clients, ch)
	}
}

func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	return s.writer.Write(p)
}

func (s *Session) Resize(cols, rows uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols = cols
	s.rows = rows
	if s.resizer != nil {
		return s.resizer.Resize(cols, rows)
	}
	return nil
}

func (s *Session) Cols() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols
}

func (s *Session) Rows() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	for ch := range s.clients {
		close(ch)
	}
	s.clients = nil
	return s.closer.Close()
}
