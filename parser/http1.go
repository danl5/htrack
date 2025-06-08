package parser

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/danl5/htrack/types"
)

// HTTP1Parser HTTP/1.x解析器
type HTTP1Parser struct {
	version types.HTTPVersion // HTTP版本
}

// NewHTTP1Parser 创建HTTP/1.x解析器
func NewHTTP1Parser() *HTTP1Parser {
	return &HTTP1Parser{}
}

// NewHTTP1ParserWithVersion 创建指定版本的HTTP/1.x解析器
func NewHTTP1ParserWithVersion(version types.HTTPVersion) *HTTP1Parser {
	return &HTTP1Parser{
		version: version,
	}
}

// ParseRequest 解析HTTP请求
func (p *HTTP1Parser) ParseRequest(connectionID string, data []byte) ([]*types.HTTPRequest, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}

	// 安全检查：限制数据大小
	if len(data) > 10*1024*1024 { // 10MB限制
		return nil, errors.New("request too large")
	}

	// 查找请求头结束位置，支持多种换行符
	headerEnd := findHeaderEnd(data)
	if headerEnd == -1 {
		return nil, errors.New("incomplete request headers")
	}

	// 安全检查：限制头部大小
	if headerEnd > 64*1024 { // 64KB头部限制
		return nil, errors.New("request headers too large")
	}

	// 分离头部和体部
	headerData := data[:headerEnd]
	bodyData := data[headerEnd+4:]

	// 解析请求行
	lines := splitHeaderLines(headerData)
	if len(lines) == 0 {
		return nil, errors.New("no request line found")
	}

	// 安全检查：限制头部字段数量
	if len(lines) > 100 { // 最多100个头部字段
		return nil, errors.New("too many header fields")
	}

	method, path, proto, err := ParseRequestLine(lines[0])
	if err != nil {
		return nil, err
	}

	// 解析URL
	parsedURL, err := url.Parse(path)
	if err != nil {
		return nil, err
	}

	// 解析协议版本
	protoMajor, protoMinor, err := parseProtocolVersion(proto)
	if err != nil {
		return nil, err
	}

	// 解析请求头
	headers := make(http.Header)
	headerCount := 0
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if len(line) == 0 {
			continue
		}

		// 安全检查：限制单个头部字段长度
		if len(line) > 8192 { // 8KB单个头部限制
			return nil, errors.New("header field too long")
		}

		colonIdx := bytes.IndexByte(line, ':')
		if colonIdx == -1 {
			continue
		}

		key := string(bytes.TrimSpace(line[:colonIdx]))
		value := string(bytes.TrimSpace(line[colonIdx+1:]))

		// 验证头部字段名
		if !isValidHeaderName(key) {
			continue
		}

		headers.Add(key, value)
		headerCount++

		// 安全检查：限制实际头部字段数量
		if headerCount > 50 {
			return nil, errors.New("too many valid header fields")
		}
	}

	// 获取内容长度
	contentLength := GetContentLength(headers)

	// 创建请求对象
	request := &types.HTTPRequest{
		HTTPMessage: types.HTTPMessage{
			Proto:         proto,
			ProtoMajor:    protoMajor,
			ProtoMinor:    protoMinor,
			Headers:       headers,
			ContentLength: contentLength,
			Timestamp:     time.Now(),
			RawData:       make([]byte, len(data)),
			Complete:      false,
		},
		Method: method,
		URL:    parsedURL,
	}
	copy(request.RawData, data)

	// 处理请求体
	if err := p.parseRequestBody(request, bodyData); err != nil {
		return nil, err
	}

	return []*types.HTTPRequest{request}, nil
}

// ParseResponse 解析HTTP响应
func (p *HTTP1Parser) ParseResponse(connectionID string, data []byte) ([]*types.HTTPResponse, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}

	// 安全检查：限制数据大小
	if len(data) > 10*1024*1024 { // 10MB限制
		return nil, errors.New("response too large")
	}

	// 查找响应头结束位置，支持多种换行符
	headerEnd := findHeaderEnd(data)
	if headerEnd == -1 {
		return nil, errors.New("incomplete response headers")
	}

	// 安全检查：限制头部大小
	if headerEnd > 64*1024 { // 64KB头部限制
		return nil, errors.New("response headers too large")
	}

	// 分离头部和体部
	headerData := data[:headerEnd]
	bodyData := data[headerEnd+4:]

	// 解析状态行
	lines := splitHeaderLines(headerData)
	if len(lines) == 0 {
		return nil, errors.New("no status line found")
	}

	// 安全检查：限制头部字段数量
	if len(lines) > 100 { // 最多100个头部字段
		return nil, errors.New("too many header fields")
	}

	proto, statusCode, status, err := ParseStatusLine(lines[0])
	if err != nil {
		return nil, err
	}

	// 解析协议版本
	protoMajor, protoMinor, err := parseProtocolVersion(proto)
	if err != nil {
		return nil, err
	}

	// 解析响应头
	headers := make(http.Header)
	headerCount := 0
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if len(line) == 0 {
			continue
		}

		// 安全检查：限制单个头部字段长度
		if len(line) > 8192 { // 8KB单个头部限制
			return nil, errors.New("header field too long")
		}

		colonIdx := bytes.IndexByte(line, ':')
		if colonIdx == -1 {
			continue
		}

		key := string(bytes.TrimSpace(line[:colonIdx]))
		value := string(bytes.TrimSpace(line[colonIdx+1:]))

		// 验证头部字段名
		if !isValidHeaderName(key) {
			continue
		}

		headers.Add(key, value)
		headerCount++

		// 安全检查：限制实际头部字段数量
		if headerCount > 50 {
			return nil, errors.New("too many valid header fields")
		}
	}

	// 获取内容长度
	contentLength := GetContentLength(headers)

	// 检查传输编码
	transferEncoding := headers.Values("Transfer-Encoding")
	chunked := IsChunkedEncoding(headers)

	// 创建响应对象
	response := &types.HTTPResponse{
		HTTPMessage: types.HTTPMessage{
			Proto:         proto,
			ProtoMajor:    protoMajor,
			ProtoMinor:    protoMinor,
			Headers:       headers,
			ContentLength: contentLength,
			Timestamp:     time.Now(),
			RawData:       make([]byte, len(data)),
			Complete:      false,
		},
		StatusCode:       statusCode,
		Status:           status,
		TransferEncoding: transferEncoding,
		Chunked:          chunked,
		Chunks:           make([]*types.ChunkInfo, 0),
	}
	copy(response.RawData, data)

	// 处理响应体
	if err := p.parseResponseBody(response, bodyData); err != nil {
		return nil, err
	}

	return []*types.HTTPResponse{response}, nil
}

// DetectVersion 检测HTTP版本
func (p *HTTP1Parser) DetectVersion(data []byte) types.HTTPVersion {
	if len(data) < 8 {
		return types.HTTP11
	}

	// 查找第一行
	firstLineEnd := bytes.Index(data, []byte("\r\n"))
	if firstLineEnd == -1 {
		firstLineEnd = bytes.Index(data, []byte("\n"))
		if firstLineEnd == -1 {
			// 如果没有换行符，检查是否是部分数据
			if len(data) > 1024 {
				return types.Unknown // 第一行太长，可能不是HTTP
			}
			firstLineEnd = len(data)
		}
	}

	firstLine := string(data[:firstLineEnd])

	// HTTP/1.x请求行检测
	httpMethods := []string{
		"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH",
		"TRACE", "CONNECT", "PROPFIND", "PROPPATCH", "MKCOL",
		"COPY", "MOVE", "LOCK", "UNLOCK",
	}

	for _, method := range httpMethods {
		if strings.HasPrefix(firstLine, method+" ") {
			// 验证请求行格式: METHOD PATH HTTP/VERSION
			parts := strings.Fields(firstLine)
			if len(parts) >= 3 && strings.HasPrefix(parts[2], "HTTP/") {
				return parseHTTPVersion(parts[2])
			}
		}
	}

	return types.Unknown
}

// IsComplete 检查消息是否完整
func (p *HTTP1Parser) IsComplete(data []byte) bool {
	// 查找头部结束
	headerEnd := findHeaderEnd(data)
	if headerEnd == -1 {
		return false
	}

	// 解析头部获取内容长度
	headers, _, err := ParseHTTPHeaders(data)
	if err != nil {
		return false
	}

	// 检查是否使用分块编码
	if IsChunkedEncoding(headers) {
		return p.isChunkedComplete(data[headerEnd+4:])
	}

	// 检查内容长度
	contentLength := GetContentLength(headers)
	if contentLength >= 0 {
		bodyData := data[headerEnd+4:]
		return int64(len(bodyData)) >= contentLength
	}

	// 对于没有Content-Length的响应，需要依赖连接关闭
	// 这里简化处理，认为已完整
	return true
}

// GetRequiredBytes 获取需要的字节数
func (p *HTTP1Parser) GetRequiredBytes(data []byte) int {
	// 查找头部结束
	headerEnd := findHeaderEnd(data)
	if headerEnd == -1 {
		return -1 // 需要更多数据来完成头部
	}

	// 解析头部获取内容长度
	headers, _, err := ParseHTTPHeaders(data)
	if err != nil {
		return -1
	}

	// 检查是否使用分块编码
	if IsChunkedEncoding(headers) {
		return -1 // 分块编码需要特殊处理
	}

	// 检查内容长度
	contentLength := GetContentLength(headers)
	if contentLength >= 0 {
		totalRequired := headerEnd + 4 + int(contentLength)
		if len(data) < totalRequired {
			return totalRequired - len(data)
		}
	}

	return 0 // 已完整
}

// 私有方法

// parseProtocolVersion 解析协议版本
func parseProtocolVersion(proto string) (int, int, error) {
	if !strings.HasPrefix(proto, "HTTP/") {
		return 0, 0, errors.New("invalid protocol")
	}

	versionStr := proto[5:] // 移除"HTTP/"
	parts := strings.Split(versionStr, ".")
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid version format")
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}

	return major, minor, nil
}

// parseRequestBody 解析请求体
func (p *HTTP1Parser) parseRequestBody(request *types.HTTPRequest, bodyData []byte) error {
	// 检查是否使用分块编码
	if IsChunkedEncoding(request.Headers) {
		return p.parseChunkedRequestBody(request, bodyData)
	}

	if request.ContentLength == 0 {
		// 没有请求体
		request.Complete = true
		return nil
	}

	if request.ContentLength > 0 {
		// 固定长度的请求体
		if int64(len(bodyData)) >= request.ContentLength {
			request.Body = bodyData[:request.ContentLength]
			request.Complete = true
		} else {
			// 数据不完整
			request.Body = bodyData
			request.Complete = false
		}
		return nil
	}

	// ContentLength为-1，没有Content-Length头部且不是分块编码
	// 对于请求来说，通常意味着没有请求体（如GET请求）
	request.Complete = true
	return nil
}

// parseResponseBody 解析响应体
func (p *HTTP1Parser) parseResponseBody(response *types.HTTPResponse, bodyData []byte) error {
	if response.Chunked {
		// 分块传输编码
		return p.parseChunkedResponseBody(response, bodyData)
	}

	if response.ContentLength == 0 {
		// 没有响应体
		response.Complete = true
		return nil
	}

	if response.ContentLength > 0 {
		// 固定长度的响应体
		if int64(len(bodyData)) >= response.ContentLength {
			response.Body = bodyData[:response.ContentLength]
			response.Complete = true
		} else {
			// 数据不完整
			response.Body = bodyData
			response.Complete = false
		}
		return nil
	}

	// 没有Content-Length，读取所有可用数据
	// 这种情况下需要依赖连接关闭来确定响应结束
	response.Body = bodyData
	// 注意：这里不能设置Complete=true，需要外部判断

	return nil
}

// parseChunkedRequestBody 解析分块请求体
func (p *HTTP1Parser) parseChunkedRequestBody(request *types.HTTPRequest, bodyData []byte) error {
	body, chunks, complete, err := p.parseChunkedData(bodyData)
	if err != nil {
		return err
	}

	request.Body = body
	request.Complete = complete

	// 将chunks转换为ChunkInfo（如果需要的话）
	// 这里简化处理
	_ = chunks

	return nil
}

// parseChunkedResponseBody 解析分块响应体
func (p *HTTP1Parser) parseChunkedResponseBody(response *types.HTTPResponse, bodyData []byte) error {
	body, chunks, complete, err := p.parseChunkedData(bodyData)
	if err != nil {
		return err
	}

	response.Body = body
	response.Complete = complete

	// 转换chunks
	for _, chunk := range chunks {
		chunkInfo := &types.ChunkInfo{
			Size:      int64(len(chunk)),
			Data:      chunk,
			Timestamp: time.Now(),
		}
		response.Chunks = append(response.Chunks, chunkInfo)
	}

	return nil
}

// parseChunkedData 解析分块数据
func (p *HTTP1Parser) parseChunkedData(data []byte) (body []byte, chunks [][]byte, complete bool, err error) {
	var bodyBuffer bytes.Buffer
	chunks = make([][]byte, 0)
	offset := 0
	maxChunks := 1000                       // 限制最大块数量
	maxBodySize := int64(100 * 1024 * 1024) // 100MB限制

	for offset < len(data) {
		// 安全检查：限制块数量
		if len(chunks) >= maxChunks {
			return nil, nil, false, errors.New("too many chunks")
		}

		// 查找块大小行的结束，支持多种换行符
		crlfIdx := findLineEnd(data[offset:])
		if crlfIdx == -1 {
			// 数据不完整
			return bodyBuffer.Bytes(), chunks, false, nil
		}

		// 解析块大小
		sizeLine := data[offset : offset+crlfIdx]
		chunkSize, err := ParseChunkSize(sizeLine)
		if err != nil {
			return nil, nil, false, err
		}

		// 安全检查：限制单个块大小
		if chunkSize > 10*1024*1024 { // 10MB单块限制
			return nil, nil, false, errors.New("chunk too large")
		}

		// 安全检查：限制总体大小
		if int64(bodyBuffer.Len())+chunkSize > maxBodySize {
			return nil, nil, false, errors.New("chunked body too large")
		}

		lineEndSize := getLineEndSize(data[offset+crlfIdx:])
		offset += crlfIdx + lineEndSize

		if chunkSize == 0 {
			// 最后一个块，解析trailer headers
			trailerHeaders, trailerEnd := parseTrailerHeaders(data[offset:])
			if trailerEnd == -1 {
				// 尾部头不完整
				return bodyBuffer.Bytes(), chunks, false, nil
			}

			// 可以在这里处理trailer headers（如果需要的话）
			_ = trailerHeaders

			return bodyBuffer.Bytes(), chunks, true, nil
		}

		// 检查是否有足够的数据读取块内容和后续的CRLF
		if offset+int(chunkSize)+2 > len(data) {
			// 数据不完整（包括块数据和后续CRLF）
			return bodyBuffer.Bytes(), chunks, false, nil
		}

		// 读取块数据
		chunkData := data[offset : offset+int(chunkSize)]
		bodyBuffer.Write(chunkData)
		chunks = append(chunks, chunkData)

		offset += int(chunkSize)

		// 跳过块后的CRLF
		lineEnd := findLineEnd(data[offset:])
		if lineEnd == -1 {
			// 缺少CRLF，数据不完整
			return bodyBuffer.Bytes(), chunks, false, nil
		}
		offset += lineEnd + getLineEndSize(data[offset+lineEnd:])
	}

	return bodyBuffer.Bytes(), chunks, false, nil
}

// isChunkedComplete 检查分块传输是否完整
func (p *HTTP1Parser) isChunkedComplete(data []byte) bool {
	_, _, complete, _ := p.parseChunkedData(data)
	return complete
}

func parseHTTPVersion(versionStr string) types.HTTPVersion {
	switch {
	case strings.HasPrefix(versionStr, "HTTP/1.0"):
		return types.HTTP10
	case strings.HasPrefix(versionStr, "HTTP/1.1"):
		return types.HTTP11
	default:
		return types.Unknown
	}
}

// findLineEnd 查找行结束位置
func findLineEnd(data []byte) int {
	if idx := bytes.Index(data, []byte("\r\n")); idx != -1 {
		return idx
	}
	if idx := bytes.IndexByte(data, '\n'); idx != -1 {
		return idx
	}
	return -1
}

// getLineEndSize 获取行结束符的大小
func getLineEndSize(data []byte) int {
	if len(data) >= 2 && data[0] == '\r' && data[1] == '\n' {
		return 2
	}
	if len(data) >= 1 && data[0] == '\n' {
		return 1
	}
	return 0
}

// parseTrailerHeaders 解析trailer headers
func parseTrailerHeaders(data []byte) (http.Header, int) {
	headers := make(http.Header)

	// 对于空trailer，只需要检查是否有单个换行符
	if len(data) >= 2 && data[0] == '\r' && data[1] == '\n' {
		// 检查是否是空trailer（只有\r\n）
		if len(data) == 2 || (len(data) > 2 && data[2] != '\r' && data[2] != '\n') {
			// 这是空trailer的结束
			return headers, 2
		}
	} else if len(data) >= 1 && data[0] == '\n' {
		// 检查是否是空trailer（只有\n）
		if len(data) == 1 || (len(data) > 1 && data[1] != '\n') {
			// 这是空trailer的结束
			return headers, 1
		}
	}

	// 查找trailer headers的结束（双换行符）
	trailerEnd := findHeaderEnd(data)
	if trailerEnd == -1 {
		return headers, -1 // 数据不完整
	}

	if trailerEnd == 0 {
		// 没有trailer headers，只有双换行符
		// 需要跳过这个双换行符
		if len(data) >= 2 && data[0] == '\r' && data[1] == '\n' {
			return headers, 2
		} else if len(data) >= 1 && data[0] == '\n' {
			return headers, 1
		}
		return headers, 0
	}

	// 解析trailer headers
	trailerData := data[:trailerEnd]
	lines := splitHeaderLines(trailerData)

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		colonIdx := bytes.IndexByte(line, ':')
		if colonIdx == -1 {
			continue
		}

		key := string(bytes.TrimSpace(line[:colonIdx]))
		value := string(bytes.TrimSpace(line[colonIdx+1:]))

		if isValidHeaderName(key) {
			headers.Add(key, value)
		}
	}

	// 返回trailer headers结束位置加上双换行符的长度
	return headers, trailerEnd + getLineEndSize(data[trailerEnd:])
}
