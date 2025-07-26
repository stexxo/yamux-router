package yamuxrouter

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
)

type ServerCfg struct {
	Listener       net.Listener
	ConfigureYamux func(cfg *yamux.Config)
	UnknownHandler MessageHandler
}

type Server struct {
	listener net.Listener

	configureYamux func(cfg *yamux.Config)
	closed         chan struct{}

	sessionsMu sync.RWMutex
	sessions   map[string]*yamux.Session

	handlersMu     sync.RWMutex
	handlers       map[MessageType]MessageHandler
	unknownHandler MessageHandler
	errHandler     func(rw ResponseWriter, msg *Message, err error)
}

func NewServer(cfg *ServerCfg) (*Server, error) {
	return &Server{
		listener:       cfg.Listener,
		unknownHandler: cfg.UnknownHandler,
	}, nil
}

func (s *Server) AddHandler(t MessageType, h MessageHandler) error {
	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()

	_, ok := s.handlers[t]
	if ok {
		return errors.New("handler for message type is already defined")
	}

	s.handlers[t] = h

	return nil
}

func (s *Server) ListenAndServe() error {
	s.closed = make(chan struct{})
	for {
		conn, err := s.listener.Accept()
		if errors.Is(err, net.ErrClosed) {
			break
		} else if err != nil {
			continue
		}

		yamuxCfg := yamux.DefaultConfig()
		if s.configureYamux != nil {
			s.configureYamux(yamuxCfg)
		}

		session, err := yamux.Server(conn, yamuxCfg)
		if err != nil {
			continue
		}

		go s.serveSession(session)
	}

	close(s.closed)
	return net.ErrClosed
}

func (s *Server) serveSession(session *yamux.Session) {
	sessionId := uuid.NewString()
	s.sessionsMu.Lock()
	s.sessions[sessionId] = session

	for {
		select {
		case <-session.CloseChan():
			s.sessionsMu.Lock()
			delete(s.sessions, sessionId)
			s.sessionsMu.Unlock()
			return
		default:
			stream, err := session.AcceptStream()
			if err != nil {
				continue
			}
			go s.serveStream(sessionId, stream)
		}
	}
}

func (s *Server) serveStream(sessionId string, stream *yamux.Stream) {
	for {
		msg, err := newMessage(context.Background(), sessionId, stream)
		if errors.Is(err, io.EOF) {
			return
		} else if err != nil {
			continue
		}

		var handler MessageHandler
		var ok bool
		s.handlersMu.RLock()
		handler, ok = s.handlers[msg.msgType]
		s.handlersMu.RUnlock()

		if !ok {
			handler = s.unknownHandler
		}

		resp := NewResponse(stream, msg.Type())

		handler(resp, msg)

		err = errors.Join(resp.Close(), msg.body.Close())
		if err != nil {
			s.errHandler(resp, msg, err)
		}
	}
}

func (s *Server) Close() error {
	if s.closed == nil {
		return nil
	}

	select {
	case _, ok := <-s.closed:
		if !ok {
			return nil
		}
	default:
	}

	err := s.listener.Close()
	<-s.closed
	return err
}
