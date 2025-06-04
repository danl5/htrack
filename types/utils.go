package types

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"time"
)

// 错误定义
var (
	ErrInvalidRequest     = errors.New("invalid HTTP request")
	ErrInvalidResponse    = errors.New("invalid HTTP response")
	ErrInvalidRequestLine = errors.New("invalid HTTP request line")
	ErrInvalidStatusLine  = errors.New("invalid HTTP status line")
	ErrIncompleteData     = errors.New("incomplete data")
	ErrInvalidChunk       = errors.New("invalid chunk data")
	ErrStreamClosed       = errors.New("stream is closed")
	ErrInvalidStreamID    = errors.New("invalid stream ID")
)

// findHeaderEnd 查找HTTP头结束位置（\r\n\r\n）
func findHeaderEnd(data []byte) int {
	return bytes.Index(data, []byte("\r\n\r\n"))
}

// findCRLF 查找CRLF位置
func findCRLF(data []byte) int {
	return bytes.Index(data, []byte("\r\n"))
}

// findByte 查找字节位置
func findByte(data []byte, b byte) int {
	return bytes.IndexByte(data, b)
}

// splitLines 按行分割数据
func splitLines(data []byte) [][]byte {
	return bytes.Split(data, []byte("\r\n"))
}

// splitBySpace 按空格分割数据
func splitBySpace(data []byte) [][]byte {
	return bytes.Fields(data)
}

// trimSpace 去除首尾空白字符
func trimSpace(data []byte) []byte {
	return bytes.TrimSpace(data)
}

// joinBytes 用分隔符连接字节切片
func joinBytes(parts [][]byte, sep byte) []byte {
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		return parts[0]
	}

	var result []byte
	for i, part := range parts {
		if i > 0 {
			result = append(result, sep)
		}
		result = append(result, part...)
	}
	return result
}

// parseInt 解析整数
func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

// parseInt64 解析64位整数
func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// parseHexInt64 解析16进制整数
func parseHexInt64(s string) (int64, error) {
	// 移除可能的扩展信息
	if idx := strings.Index(s, ";"); idx != -1 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	return strconv.ParseInt(s, 16, 64)
}

// isValidHTTPMethod 检查是否是有效的HTTP方法
func isValidHTTPMethod(method string) bool {
	validMethods := []string{
		"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS",
		"PATCH", "TRACE", "CONNECT",
	}

	for _, valid := range validMethods {
		if method == valid {
			return true
		}
	}
	return false
}

// isValidStatusCode 检查是否是有效的HTTP状态码
func isValidStatusCode(code int) bool {
	return code >= 100 && code < 600
}

// detectHTTPVersion 检测HTTP版本
func detectHTTPVersion(data []byte) HTTPVersion {
	// 查找第一行
	lineEnd := bytes.Index(data, []byte("\r\n"))
	if lineEnd == -1 {
		lineEnd = bytes.Index(data, []byte("\n"))
		if lineEnd == -1 {
			return HTTP11 // 默认
		}
	}

	firstLine := data[:lineEnd]

	// HTTP/2的识别通常需要更复杂的逻辑，这里简化处理
	if bytes.Contains(firstLine, []byte("HTTP/2")) {
		return HTTP2
	} else if bytes.Contains(firstLine, []byte("HTTP/1.0")) {
		return HTTP10
	} else if bytes.Contains(firstLine, []byte("HTTP/1.1")) {
		return HTTP11
	}

	// 默认返回HTTP/1.1
	return HTTP11
}

// isRequestData 判断数据是否是HTTP请求
func isRequestData(data []byte) bool {
	if len(data) < 4 {
		return false
	}

	// 查找第一个空格
	spaceIdx := bytes.IndexByte(data, ' ')
	if spaceIdx == -1 || spaceIdx > 10 {
		return false
	}

	method := string(data[:spaceIdx])
	return isValidHTTPMethod(method)
}

// isResponseData 判断数据是否是HTTP响应
func isResponseData(data []byte) bool {
	if len(data) < 8 {
		return false
	}

	// HTTP响应以"HTTP/"开头
	return bytes.HasPrefix(data, []byte("HTTP/"))
}

// calculateContentLength 计算内容长度
func calculateContentLength(headers map[string][]string) int64 {
	if contentLength, exists := headers["Content-Length"]; exists && len(contentLength) > 0 {
		if length, err := strconv.ParseInt(contentLength[0], 10, 64); err == nil {
			return length
		}
	}
	return -1
}

// isChunkedEncoding 检查是否使用分块传输编码
func isChunkedEncoding(headers map[string][]string) bool {
	if transferEncoding, exists := headers["Transfer-Encoding"]; exists {
		for _, encoding := range transferEncoding {
			if strings.ToLower(encoding) == "chunked" {
				return true
			}
		}
	}
	return false
}

// normalizeHeaderKey 规范化头部键名
func normalizeHeaderKey(key string) string {
	// HTTP头部键名不区分大小写，但通常使用Title-Case
	parts := strings.Split(strings.ToLower(key), "-")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "-")
}

// copyBytes 安全复制字节切片
func copyBytes(src []byte) []byte {
	if src == nil {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// mergeHeaders 合并HTTP头部
func mergeHeaders(dst, src map[string][]string) {
	for key, values := range src {
		normalizedKey := normalizeHeaderKey(key)
		for _, value := range values {
			dst[normalizedKey] = append(dst[normalizedKey], value)
		}
	}
}

// validateStreamID 验证HTTP/2流ID
func validateStreamID(streamID uint32, isClient bool) error {
	if streamID == 0 {
		return ErrInvalidStreamID
	}

	// 客户端发起的流ID必须是奇数，服务器发起的必须是偶数
	if isClient {
		if streamID%2 == 0 {
			return ErrInvalidStreamID
		}
	} else {
		if streamID%2 == 1 {
			return ErrInvalidStreamID
		}
	}

	return nil
}

// generateTransactionID 生成事务ID
func generateTransactionID(connectionID string, streamID *uint32) string {
	if streamID != nil {
		return connectionID + "-" + strconv.FormatUint(uint64(*streamID), 10)
	}
	return connectionID + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// generateConnectionID 生成连接ID
func generateConnectionID(localAddr, remoteAddr string) string {
	return localAddr + "->" + remoteAddr + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}
