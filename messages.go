package yamuxrouter

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
)

type MessageHandler func(rw ResponseWriter, msg *Message)
type MessageType uint32
type MessageLength uint64

// ResponseWriter controls the Response to a message
type ResponseWriter interface {
	WriteHeader(msgType MessageType, msgLength MessageLength, stream bool) error
	io.WriteCloser
}

type Response struct {
	messageType MessageType

	output io.Writer
	length MessageLength
	stream bool

	buf bytes.Buffer

	writingStarted bool
	closed         bool
}

func NewResponse(stream io.Writer, defaultMessageType MessageType) *Response {
	out := &Response{
		messageType: defaultMessageType,
		output:      stream,
		stream:      false,
	}
	return out
}

func (r *Response) Write(p []byte) (n int, err error) {
	if !r.writingStarted && r.stream {
		err = r.writeHeaderToStream()
		if err != nil {
			return 0, err
		}
	}

	r.writingStarted = true

	if !r.stream {
		n, err = r.buf.Write(p)
	} else {
		if uint64(len(p)) <= uint64(r.length) {
			n, err = r.output.Write(p)
		} else if r.length == 0 {
			n = 0
			err = errors.New("length has been reached")
		} else {
			n, err = r.output.Write(p[:r.length])
			err = errors.Join(err, io.ErrShortWrite)
		}
	}

	return n, err
}

func (r *Response) WriteHeader(msgType MessageType, msgLength MessageLength, stream bool) error {
	if r.writingStarted {
		return errors.New("writing of message body has already been started, cannot change header")
	}
	r.stream = stream
	r.messageType = msgType
	if !stream {
		r.length = 0
	} else {
		r.length = msgLength
	}
	return nil
}

func (r *Response) writeHeaderToStream() error {
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

func (r *Response) Close() error {
	if r.closed {
		return nil
	}

	if r.stream {
		if r.length != 0 {
			_, err := r.output.Write(make([]byte, r.length))
			if err != nil {
				return err
			}
		}
		r.closed = true
		return nil
	}

	r.length = MessageLength(r.buf.Len())

	err := r.writeHeaderToStream()
	if err != nil {
		return err
	}

	_, err = r.buf.WriteTo(r.output)
	if err != nil {
		return err
	}

	r.closed = true
	return nil
}

type messageReader struct {
	l      int
	body   io.Reader
	closed bool
}

func newMessageReader(l MessageLength, body io.Reader) *messageReader {
	return &messageReader{
		body: io.LimitReader(body, int64(l)),
	}
}

func (m messageReader) Read(p []byte) (n int, err error) {
	return m.body.Read(p)
}

func (m messageReader) Close() error {
	_, err := io.Copy(io.Discard, m.body)
	return err
}

type Message struct {
	context context.Context
	body    io.ReadCloser
	length  MessageLength
	msgType MessageType
}

func newMessage(ctx context.Context, stream io.Reader) (*Message, error) {
	out := &Message{
		context: ctx,
	}

	err := binary.Read(stream, binary.BigEndian, &out.msgType)
	if err != nil {
		return nil, err
	}

	err = binary.Read(stream, binary.BigEndian, &out.length)
	if err != nil {
		return nil, err
	}

	out.body = newMessageReader(out.length, stream)

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

func (m *Message) Length() MessageLength {
	return m.length
}

func (m *Message) Type() MessageType {
	return m.msgType
}
