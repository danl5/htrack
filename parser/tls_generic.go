package parser

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/danl5/htrack/types"
)

// TLS记录类型常量
const (
	TLSRecordTypeChangeCipherSpec = 0x14
	TLSRecordTypeAlert           = 0x15
	TLSRecordTypeHandshake       = 0x16
	TLSRecordTypeApplicationData = 0x17
	TLSRecordTypeHeartbeat       = 0x18
)

// TLS版本常量
const (
	TLSVersion10 = 0x0301
	TLSVersion11 = 0x0302
	TLSVersion12 = 0x0303
	TLSVersion13 = 0x0304
)

// TLSGenericParser 通用TLS协议解析器
type TLSGenericParser struct {
	version types.HTTPVersion
}

// TLSRecord TLS记录结构
type TLSRecord struct {
	Type    uint8
	Version uint16
	Length  uint16
	Data    []byte
}

// TLSConnection TLS连接状态
type TLSConnection struct {
	ID              string
	ClientHello     []byte
	ServerHello     []byte
	ApplicationData [][]byte
	CreatedAt       time.Time
	LastActivity    time.Time
}

// NewTLSGenericParser 创建通用TLS解析器
func NewTLSGenericParser() *TLSGenericParser {
	return &TLSGenericParser{
		version: types.TLS_OTHER,
	}
}

// DetectVersion 检测是否为TLS协议
func (p *TLSGenericParser) DetectVersion(data []byte) types.HTTPVersion {
	if len(data) < 5 {
		return types.Unknown
	}

	// 检查TLS记录头格式
	recordType := data[0]
	version := binary.BigEndian.Uint16(data[1:3])
	length := binary.BigEndian.Uint16(data[3:5])

	// 验证TLS记录类型
	if !isValidTLSRecordType(recordType) {
		return types.Unknown
	}

	// 验证TLS版本
	if !isValidTLSVersion(version) {
		return types.Unknown
	}

	// 验证长度合理性（TLS记录最大16KB）
	if length > 16384 {
		return types.Unknown
	}

	// 检查数据长度是否足够
	if len(data) < int(5+length) {
		// 数据不完整，但格式正确，可能是TLS
		return types.TLS_OTHER
	}

	return types.TLS_OTHER
}

// ParseTLSRecord 解析TLS记录
func (p *TLSGenericParser) ParseTLSRecord(data []byte) (*TLSRecord, error) {
	if len(data) < 5 {
		return nil, errors.New("insufficient data for TLS record header")
	}

	record := &TLSRecord{
		Type:    data[0],
		Version: binary.BigEndian.Uint16(data[1:3]),
		Length:  binary.BigEndian.Uint16(data[3:5]),
	}

	// 检查数据长度
	if len(data) < int(5+record.Length) {
		return nil, errors.New("insufficient data for TLS record payload")
	}

	record.Data = make([]byte, record.Length)
	copy(record.Data, data[5:5+record.Length])

	return record, nil
}

// ParseRequest 解析TLS请求数据
func (p *TLSGenericParser) ParseRequest(connectionID string, data []byte, packetInfo *types.PacketInfo) ([]*types.HTTPRequest, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}

	// 解析TLS记录
	records, err := p.parseTLSRecords(data)
	if err != nil {
		return nil, err
	}

	var requests []*types.HTTPRequest
	for _, record := range records {
		req := &types.HTTPRequest{
			HTTPMessage: types.HTTPMessage{
				Proto:       "TLS/Other",
				ProtoMajor:  1,
				ProtoMinor:  0,
				Headers:     make(map[string][]string),
				Body:        record.Data,
				Complete:    true,
				Timestamp:   time.Now(),
				TCPTuple:    packetInfo.TCPTuple,
				PID:         packetInfo.PID,
				ProcessName: packetInfo.ProcessName,
			},
			Method: "TLS",
			URL:    nil, // TLS没有URL概念
		}

		// 添加TLS特定的元数据
		req.Headers["TLS-Record-Type"] = []string{fmt.Sprintf("%d", record.Type)}
		req.Headers["TLS-Version"] = []string{fmt.Sprintf("0x%04x", record.Version)}
		req.Headers["TLS-Length"] = []string{fmt.Sprintf("%d", record.Length)}

		requests = append(requests, req)
	}

	return requests, nil
}

// ParseResponse 解析TLS响应数据
func (p *TLSGenericParser) ParseResponse(connectionID string, data []byte, packetInfo *types.PacketInfo) ([]*types.HTTPResponse, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}

	// 解析TLS记录
	records, err := p.parseTLSRecords(data)
	if err != nil {
		return nil, err
	}

	var responses []*types.HTTPResponse
	for _, record := range records {
		resp := &types.HTTPResponse{
			HTTPMessage: types.HTTPMessage{
				Proto:       "TLS/Other",
				ProtoMajor:  1,
				ProtoMinor:  0,
				Headers:     make(map[string][]string),
				Body:        record.Data,
				Complete:    true,
				Timestamp:   time.Now(),
				TCPTuple:    packetInfo.TCPTuple,
				PID:         packetInfo.PID,
				ProcessName: packetInfo.ProcessName,
			},
			Status:     "TLS Record",
			StatusCode: int(record.Type), // 使用记录类型作为状态码
		}

		// 添加TLS特定的元数据
		resp.Headers["TLS-Record-Type"] = []string{fmt.Sprintf("%d", record.Type)}
		resp.Headers["TLS-Version"] = []string{fmt.Sprintf("0x%04x", record.Version)}
		resp.Headers["TLS-Length"] = []string{fmt.Sprintf("%d", record.Length)}

		responses = append(responses, resp)
	}

	return responses, nil
}

// parseTLSRecords 解析多个TLS记录
func (p *TLSGenericParser) parseTLSRecords(data []byte) ([]*TLSRecord, error) {
	var records []*TLSRecord
	offset := 0

	for offset < len(data) {
		if len(data[offset:]) < 5 {
			break // 数据不足，停止解析
		}

		record, err := p.ParseTLSRecord(data[offset:])
		if err != nil {
			return records, err
		}

		records = append(records, record)
		offset += int(5 + record.Length)
	}

	return records, nil
}

// IsComplete 检查TLS数据是否完整
func (p *TLSGenericParser) IsComplete(data []byte) bool {
	if len(data) < 5 {
		return false
	}

	length := binary.BigEndian.Uint16(data[3:5])
	return len(data) >= int(5+length)
}

// GetRequiredBytes 获取需要的字节数
func (p *TLSGenericParser) GetRequiredBytes(data []byte) int {
	if len(data) < 5 {
		return 5 - len(data)
	}

	length := binary.BigEndian.Uint16(data[3:5])
	required := int(5 + length)
	if len(data) < required {
		return required - len(data)
	}

	return 0
}

// 辅助函数

// isValidTLSRecordType 检查TLS记录类型是否有效
func isValidTLSRecordType(recordType uint8) bool {
	switch recordType {
	case TLSRecordTypeChangeCipherSpec,
		TLSRecordTypeAlert,
		TLSRecordTypeHandshake,
		TLSRecordTypeApplicationData,
		TLSRecordTypeHeartbeat:
		return true
	default:
		return false
	}
}

// isValidTLSVersion 检查TLS版本是否有效
func isValidTLSVersion(version uint16) bool {
	switch version {
	case TLSVersion10, TLSVersion11, TLSVersion12, TLSVersion13:
		return true
	default:
		// 也接受一些常见的变体
		return version >= 0x0300 && version <= 0x0304
	}
}

// GetTLSRecordTypeName 获取TLS记录类型名称
func GetTLSRecordTypeName(recordType uint8) string {
	switch recordType {
	case TLSRecordTypeChangeCipherSpec:
		return "ChangeCipherSpec"
	case TLSRecordTypeAlert:
		return "Alert"
	case TLSRecordTypeHandshake:
		return "Handshake"
	case TLSRecordTypeApplicationData:
		return "ApplicationData"
	case TLSRecordTypeHeartbeat:
		return "Heartbeat"
	default:
		return "Unknown"
	}
}

// GetTLSVersionName 获取TLS版本名称
func GetTLSVersionName(version uint16) string {
	switch version {
	case TLSVersion10:
		return "TLS 1.0"
	case TLSVersion11:
		return "TLS 1.1"
	case TLSVersion12:
		return "TLS 1.2"
	case TLSVersion13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("TLS 0x%04x", version)
	}
}