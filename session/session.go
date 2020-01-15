package session

import (
	"encoding/json"
	"sync"

	"github.com/baetyl/baetyl-broker/common"
	"github.com/baetyl/baetyl-broker/queue"
	"github.com/baetyl/baetyl-go/link"
	"github.com/baetyl/baetyl-go/log"
	"github.com/baetyl/baetyl-go/mqtt"
)

// Info session information
type Info struct {
	ID            string
	Will          *link.Message `json:"Will,omitempty"` // will message
	CleanSession  bool
	Subscriptions map[string]mqtt.QOS
}

func (i *Info) String() string {
	d, _ := json.Marshal(i)
	return string(d)
}

// Session session of a client
type Session struct {
	Info
	qos0 queue.Queue // queue for qos0
	qos1 queue.Queue // queue for qos1
	subs *mqtt.Trie
	clis map[string]client
	log  *log.Logger
	mu   sync.Mutex
	sync.Once
}

// Push pushes source message to session queue
func (s *Session) Push(e *common.Event) error {
	// always flow message with qos 0 into qos0 queue
	if e.Context.QOS == 0 {
		return s.qos0.Push(e)
	}
	// TODO: improve
	qs := s.subs.Match(e.Context.Topic)
	if len(qs) == 0 {
		panic("At least one subscription matched")
	}
	for _, q := range qs {
		if q.(mqtt.QOS) > 0 {
			return s.qos1.Push(e)
		}
	}
	return s.qos0.Push(e)
}

func (s *Session) clientCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clis)
}

func (s *Session) addClient(c client, exclusive bool) map[string]client {
	s.mu.Lock()
	defer s.mu.Unlock()
	var prev map[string]client
	if exclusive {
		prev = s.clis
		s.clis = map[string]client{}
	} else if len(s.clis) != 0 {
		s.log.Info("add new client to existing session", log.Any("cid", c.getID()))
	}
	s.clis[c.getID()] = c
	c.setSession(s)
	return prev
}

// returns true if session should be cleaned
func (s *Session) delClient(c client) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clis, c.getID())
	return s.CleanSession && len(s.clis) == 0
}

// Close closes session
func (s *Session) close() {
	s.Do(func() {
		s.log.Info("session is closing")
		defer s.log.Info("session has closed")
		err := s.qos0.Close()
		if err != nil {
			s.log.Warn("failed to close qos0 queue", log.Error(err))
		}
		err = s.qos1.Close()
		if err != nil {
			s.log.Warn("failed to close qos1 queue", log.Error(err))
		}
	})
}
