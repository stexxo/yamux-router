package yamuxrouter

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestBufferedResponse(t *testing.T) {
	output := new(bytes.Buffer)

	resp := NewResponse(output, 1)
	assert.NotNil(t, resp)

	msg := []byte("Hello World!")
	err := resp.WriteHeader(1, 0, false)
	assert.NoError(t, err)

	_, err = resp.Write(msg)
	assert.NoError(t, err)

	err = resp.Close()
	assert.NoError(t, err)

	parsedMessage, err := newMessage(context.Background(), "session", output)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMessage)

	parsedMessageBuf := new(bytes.Buffer)
	_, err = parsedMessageBuf.ReadFrom(parsedMessage.Body())
	assert.NoError(t, err)
}

func TestStreamResponse(t *testing.T) {
	output := new(bytes.Buffer)

	resp := NewResponse(output, 1)
	assert.NotNil(t, resp)

	msg := []byte("Hello World!")
	err := resp.WriteHeader(1, MessageLength(len(msg)), true)
	assert.NoError(t, err)

	_, err = resp.Write(msg)
	assert.NoError(t, err)

	err = resp.Close()
	assert.NoError(t, err)

	parsedMessage, err := newMessage(context.Background(), "session", output)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMessage)

	parsedMessageBuf := new(bytes.Buffer)
	_, err = parsedMessageBuf.ReadFrom(parsedMessage.Body())
	assert.NoError(t, err)
}
