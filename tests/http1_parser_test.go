package tests

import (
	"testing"

	"github.com/danl5/htrack/parser"
	"github.com/stretchr/testify/assert"
)

func TestParseHTTPRequest_SimpleGET(t *testing.T) {
	p := parser.NewHTTP1Parser()
	rawData := []byte("GET /test HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test-agent\r\n\r\n")

	req, err := p.ParseRequest("test-session-1", rawData)

	assert.NoError(t, err)
	assert.NotNil(t, req)
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, "/test", req.URL.Path)
	assert.Equal(t, "HTTP/1.1", req.Proto)
	assert.Equal(t, "example.com", req.Headers.Get("Host"))
	assert.Equal(t, "test-agent", req.Headers.Get("User-Agent"))
	assert.True(t, req.Complete)
}

func TestParseHTTPResponse_SimpleOK(t *testing.T) {
	p := parser.NewHTTP1Parser()
	rawData := []byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 12\r\n\r\nHello, world")

	resp, err := p.ParseResponse("test-session-1", rawData)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "OK", resp.Status)
	assert.Equal(t, "HTTP/1.1", resp.Proto)
	assert.Equal(t, "text/plain", resp.Headers.Get("Content-Type"))
	assert.Equal(t, int64(12), resp.ContentLength)
	assert.Equal(t, []byte("Hello, world"), resp.Body)
	assert.True(t, resp.Complete)
}

func TestParseHTTPRequest_POSTWithBody(t *testing.T) {
	p := parser.NewHTTP1Parser()
	rawData := []byte("POST /submit HTTP/1.1\r\nHost: example.com\r\nContent-Type: application/json\r\nContent-Length: 18\r\n\r\n{\"key\":\"value\"}")

	req, err := p.ParseRequest("test-session-1", rawData)

	assert.NoError(t, err)
	assert.NotNil(t, req)
	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "/submit", req.URL.Path)
	assert.Equal(t, "application/json", req.Headers.Get("Content-Type"))
	assert.Equal(t, int64(18), req.ContentLength)
	assert.Equal(t, []byte("{\"key\":\"value\"}"), req.Body)
	assert.True(t, req.Complete)
}

func TestParseHTTPRequest_Chunked(t *testing.T) {
	p := parser.NewHTTP1Parser()
	chunk1 := []byte("POST /chunked HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nWiki\r\n")
	chunk2 := []byte("5\r\npedia\r\nE\r\n in\r\n\r\nchunks.\r\n0\r\n\r\n")

	req, err := p.ParseRequest("test-session-1", chunk1)
	assert.NoError(t, err)
	assert.NotNil(t, req)
	assert.False(t, req.Complete) // Should not be complete after first chunk

	req, err = p.ParseRequest("test-session-1", append(chunk1, chunk2...))
	// 或者直接用chunk1+chunk2整体作为一次请求体传入
	assert.NoError(t, err)
	assert.NotNil(t, req)
	assert.True(t, req.Complete)
	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "/chunked", req.URL.Path)
	assert.Equal(t, "chunked", req.Headers.Get("Transfer-Encoding"))
	assert.Equal(t, []byte("Wikipedia in\r\n\r\nchunks."), req.Body)
}
