package yamuxrouter

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"

	"github.com/hashicorp/yamux"
)

// ResponseWriter controls the response to a message
type ResponseWriter interface {
	// Must be called before Write(), but does not have to be called. Defaults to message type from request, and buffers output.
	// Calling with l = -1, will continue to buffer Write() and require Flush() to be called
	// Calling with l >=0 will result in streaming writes to caller.
	Header(t MessageType, l int) error

	// Writes to either a buffer or streams response, based on whether Header() was called.
	// Truncates writes if l was set in Header()
	Write([]byte) (int, error)

	// Flushes a buffered output and will pad a response with zeroes if there were not enough bytes Written to during a non-buffered write.
	Flush() error
}

type response struct {
	messageType MessageType

	output io.Writer
	length int

	buf bytes.Buffer

	useBuffer      bool
	writingStarted bool
	flushed        bool
}

func newResponse(stream io.Writer, m *Message) *response {
	out := &response{
		messageType: m.msgType,
		output:      stream,
		useBuffer:   true,
	}
	return out
}

func (r *response) Write(p []byte) (n int, err error) {
	if !r.writingStarted && !r.useBuffer {
		err = r.writeHeader()
		if err != nil {
			return 0, err
		}
	}

	r.writingStarted = true

	if r.useBuffer {
		n, err = r.buf.Write(p)
	} else {
		if len(p) <= r.length {
			n, err = r.output.Write(p)
		} else if r.length == 0 {
			n = 0
			err = errors.New("length has beeen reached")
		} else {
			n, err = r.output.Write(p[:r.length])
			err = errors.Join(err, io.ErrShortWrite)
		}
	}

	return n, err
}

func (r *response) Header(t MessageType, l int) error {
	if r.writingStarted {
		return errors.New("can not change message header after writing has begun")
	}

	r.messageType = t

	if l != -1 {
		r.length = l
		r.useBuffer = false
	}

	return nil
}

func (r *response) writeHeader() error {
	err := binary.Write(r.output, binary.BigEndian, &r.messageType)
	if err != nil {
		return err
	}

	err = binary.Write(r.output, binary.BigEndian, &r.length)
	if err != nil {
		return err
	}

	return nil
}

func (r *response) Flush() error {
	if r.flushed {
		return nil
	}

	if !r.useBuffer {
		if r.length != 0 {
			_, err := r.output.Write(make([]byte, r.length))
			if err != nil {
				return err
			}
		}
		r.flushed = true
		return nil
	}

	r.length = r.buf.Len()

	err := r.writeHeader()
	if err != nil {
		return err
	}

	_, err = r.buf.WriteTo(r.output)
	if err != nil {
		return err
	}

	r.flushed = true

	return nil
}

type messageReader struct {
	l      int
	body   io.Reader
	closed bool
}

func newMessageReader(l uint64, body io.Reader) *messageReader {
	return &messageReader{
		body: io.LimitReader(body, int64(l)),
	}
}

// Represents a Message
type MessageType uint32

type Message struct {
	context   context.Context
	body      io.ReadCloser
	length    uint64
	msgType   MessageType
	sessionId string
}

func newMessage(ctx context.Context, sessionId string, stream *yamux.Stream) (*Message, error) {
	out := &Message{
		context:   ctx,
		sessionId: sessionId,
	}

	err := binary.Read(stream, binary.BigEndian, &out.msgType)
	if err != nil {
		return nil, err
	}

	err = binary.Read(stream, binary.BigEndian, &out.length)
	if err != nil {
		return nil, err
	}

	out.body = newMessageReader(out.length, )

	return out, nil
}

func (m *Message) WithContext(ctx context.Context) *Message {
	if ctx == nil {
		panic("nil context")
	}

	newMsg := *m
	newMsg.context = ctx
	return &newMsg
}

func (m *Message) Context() context.Context {
	return m.context
}

func (m *Message) Body() io.Reader {
	return m.body
}

func (m *Message) Length() uint64 {
	return m.length
}

func (m *Message) Type() MessageType {
	return m.msgType
}

func (m *Message) SessionId() string {
	return m.sessionId
}

type MessageHandler func(ResponseWriter, msg *Message)
