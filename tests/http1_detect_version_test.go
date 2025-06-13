package tests

import (
	"testing"

	"github.com/danl5/htrack/parser"
	"github.com/danl5/htrack/types"
	"github.com/stretchr/testify/assert"
)

func TestHTTP1DetectVersion_Request(t *testing.T) {
	p := parser.NewHTTP1Parser()

	// 测试HTTP/1.1请求
	data := []byte("GET /test HTTP/1.1\r\nHost: example.com\r\n\r\n")
	version := p.DetectVersion(data)
	assert.Equal(t, types.HTTP11, version)

	// 测试HTTP/1.0请求
	data = []byte("POST /api HTTP/1.0\r\nContent-Length: 0\r\n\r\n")
	version = p.DetectVersion(data)
	assert.Equal(t, types.HTTP10, version)

	// 测试其他HTTP方法
	data = []byte("PUT /resource HTTP/1.1\r\nHost: example.com\r\n\r\n")
	version = p.DetectVersion(data)
	assert.Equal(t, types.HTTP11, version)

	data = []byte("DELETE /resource HTTP/1.1\r\nHost: example.com\r\n\r\n")
	version = p.DetectVersion(data)
	assert.Equal(t, types.HTTP11, version)
}

func TestHTTP1DetectVersion_Response(t *testing.T) {
	p := parser.NewHTTP1Parser()

	// 测试HTTP/1.1响应
	data := []byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\n")
	version := p.DetectVersion(data)
	assert.Equal(t, types.HTTP11, version)

	// 测试HTTP/1.0响应
	data = []byte("HTTP/1.0 404 Not Found\r\nContent-Type: text/html\r\n\r\n")
	version = p.DetectVersion(data)
	assert.Equal(t, types.HTTP10, version)

	// 测试其他状态码
	data = []byte("HTTP/1.1 500 Internal Server Error\r\nContent-Type: text/plain\r\n\r\n")
	version = p.DetectVersion(data)
	assert.Equal(t, types.HTTP11, version)
}

func TestHTTP1DetectVersion_InsufficientData(t *testing.T) {
	p := parser.NewHTTP1Parser()

	// 测试数据不足的情况
	data := []byte("GET")
	version := p.DetectVersion(data)
	assert.Equal(t, types.Unknown, version)

	// 测试空数据
	data = []byte("")
	version = p.DetectVersion(data)
	assert.Equal(t, types.Unknown, version)

	// 测试没有换行符的不完整数据
	data = []byte("GET /test HTTP/1.1")
	version = p.DetectVersion(data)
	assert.Equal(t, types.Unknown, version)
}

func TestHTTP1DetectVersion_InvalidData(t *testing.T) {
	p := parser.NewHTTP1Parser()

	// 测试无效的HTTP方法
	data := []byte("INVALID /test HTTP/1.1\r\n\r\n")
	version := p.DetectVersion(data)
	assert.Equal(t, types.Unknown, version)

	// 测试格式错误的请求行
	data = []byte("GET\r\n\r\n")
	version = p.DetectVersion(data)
	assert.Equal(t, types.Unknown, version)

	// 测试无效的状态码
	data = []byte("HTTP/1.1 ABC OK\r\n\r\n")
	version = p.DetectVersion(data)
	assert.Equal(t, types.Unknown, version)

	// 测试状态码长度错误
	data = []byte("HTTP/1.1 20 OK\r\n\r\n")
	version = p.DetectVersion(data)
	assert.Equal(t, types.Unknown, version)

	// 测试第一行过长
	longLine := make([]byte, 9000)
	for i := range longLine {
		longLine[i] = 'A'
	}
	longLine = append(longLine, []byte("\r\n\r\n")...)
	version = p.DetectVersion(longLine)
	assert.Equal(t, types.Unknown, version)
}

func TestHTTP1DetectVersion_EdgeCases(t *testing.T) {
	p := parser.NewHTTP1Parser()

	// 测试WebDAV方法
	data := []byte("PROPFIND /dav HTTP/1.1\r\nHost: example.com\r\n\r\n")
	version := p.DetectVersion(data)
	assert.Equal(t, types.HTTP11, version)

	data = []byte("MKCOL /collection HTTP/1.1\r\nHost: example.com\r\n\r\n")
	version = p.DetectVersion(data)
	assert.Equal(t, types.HTTP11, version)

	// 测试只有\n换行符的情况
	data = []byte("GET /test HTTP/1.1\nHost: example.com\n\n")
	version = p.DetectVersion(data)
	assert.Equal(t, types.HTTP11, version)

	// 测试响应只有\n换行符
	data = []byte("HTTP/1.1 200 OK\nContent-Type: text/plain\n\n")
	version = p.DetectVersion(data)
	assert.Equal(t, types.HTTP11, version)
}