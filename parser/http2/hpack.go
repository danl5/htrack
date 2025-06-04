package http2

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
)

// HPACK解码器
type HPACKDecoder struct {
	dynamicTable *DynamicTable
	maxTableSize uint32
}

// HPACK编码器
type HPACKEncoder struct {
	dynamicTable *DynamicTable
	maxTableSize uint32
}

// DynamicTable HPACK动态表
type DynamicTable struct {
	entries   []HeaderField
	size      uint32
	maxSize   uint32
	insertPos int
}

// HeaderField 头部字段
type HeaderField struct {
	Name  string
	Value string
}

// 静态表（RFC 7541 Appendix B）
var staticTable = []HeaderField{
	{"", ""},                             // 索引0（未使用）
	{":authority", ""},                   // 1
	{":method", "GET"},                   // 2
	{":method", "POST"},                  // 3
	{":path", "/"},                       // 4
	{":path", "/index.html"},             // 5
	{":scheme", "http"},                  // 6
	{":scheme", "https"},                 // 7
	{":status", "200"},                   // 8
	{":status", "204"},                   // 9
	{":status", "206"},                   // 10
	{":status", "304"},                   // 11
	{":status", "400"},                   // 12
	{":status", "404"},                   // 13
	{":status", "500"},                   // 14
	{"accept-charset", ""},               // 15
	{"accept-encoding", "gzip, deflate"}, // 16
	{"accept-language", ""},              // 17
	{"accept-ranges", ""},                // 18
	{"accept", ""},                       // 19
	{"access-control-allow-origin", ""},  // 20
	{"age", ""},                          // 21
	{"allow", ""},                        // 22
	{"authorization", ""},                // 23
	{"cache-control", ""},                // 24
	{"content-disposition", ""},          // 25
	{"content-encoding", ""},             // 26
	{"content-language", ""},             // 27
	{"content-length", ""},               // 28
	{"content-location", ""},             // 29
	{"content-range", ""},                // 30
	{"content-type", ""},                 // 31
	{"cookie", ""},                       // 32
	{"date", ""},                         // 33
	{"etag", ""},                         // 34
	{"expect", ""},                       // 35
	{"expires", ""},                      // 36
	{"from", ""},                         // 37
	{"host", ""},                         // 38
	{"if-match", ""},                     // 39
	{"if-modified-since", ""},            // 40
	{"if-none-match", ""},                // 41
	{"if-range", ""},                     // 42
	{"if-unmodified-since", ""},          // 43
	{"last-modified", ""},                // 44
	{"link", ""},                         // 45
	{"location", ""},                     // 46
	{"max-forwards", ""},                 // 47
	{"proxy-authenticate", ""},           // 48
	{"proxy-authorization", ""},          // 49
	{"range", ""},                        // 50
	{"referer", ""},                      // 51
	{"refresh", ""},                      // 52
	{"retry-after", ""},                  // 53
	{"server", ""},                       // 54
	{"set-cookie", ""},                   // 55
	{"strict-transport-security", ""},    // 56
	{"transfer-encoding", ""},            // 57
	{"user-agent", ""},                   // 58
	{"vary", ""},                         // 59
	{"via", ""},                          // 60
	{"www-authenticate", ""},             // 61
}

// NewHPACKDecoder 创建HPACK解码器
func NewHPACKDecoder(maxTableSize uint32) *HPACKDecoder {
	return &HPACKDecoder{
		dynamicTable: NewDynamicTable(maxTableSize),
		maxTableSize: maxTableSize,
	}
}

// NewHPACKEncoder 创建HPACK编码器
func NewHPACKEncoder(maxTableSize uint32) *HPACKEncoder {
	return &HPACKEncoder{
		dynamicTable: NewDynamicTable(maxTableSize),
		maxTableSize: maxTableSize,
	}
}

// NewDynamicTable 创建动态表
func NewDynamicTable(maxSize uint32) *DynamicTable {
	return &DynamicTable{
		entries:   make([]HeaderField, 0),
		size:      0,
		maxSize:   maxSize,
		insertPos: 0,
	}
}

// DecodeFull 解码完整的头部块
func (d *HPACKDecoder) DecodeFull(data []byte) (http.Header, error) {
	headers := make(http.Header)
	buf := bytes.NewReader(data)

	for buf.Len() > 0 {
		field, err := d.decodeHeaderField(buf)
		if err != nil {
			return nil, err
		}
		headers.Add(field.Name, field.Value)
	}

	return headers, nil
}

// decodeHeaderField 解码单个头部字段
func (d *HPACKDecoder) decodeHeaderField(buf *bytes.Reader) (HeaderField, error) {
	b, err := buf.ReadByte()
	if err != nil {
		return HeaderField{}, err
	}

	// 索引头部字段表示
	if b&0x80 != 0 {
		buf.UnreadByte()
		index, err := d.decodeInteger(buf, 7)
		if err != nil {
			return HeaderField{}, err
		}
		return d.getIndexedField(index)
	}

	// 字面头部字段表示 - 增量索引
	if b&0x40 != 0 {
		buf.UnreadByte()
		return d.decodeLiteralField(buf, 6, true)
	}

	// 字面头部字段表示 - 不索引
	if b&0x10 == 0 {
		buf.UnreadByte()
		return d.decodeLiteralField(buf, 4, false)
	}

	// 字面头部字段表示 - 从不索引
	buf.UnreadByte()
	return d.decodeLiteralField(buf, 4, false)
}

// decodeLiteralField 解码字面头部字段
func (d *HPACKDecoder) decodeLiteralField(buf *bytes.Reader, prefixBits int, addToTable bool) (HeaderField, error) {
	index, err := d.decodeInteger(buf, prefixBits)
	if err != nil {
		return HeaderField{}, err
	}

	var name string
	if index == 0 {
		// 新名称
		name, err = d.decodeString(buf)
		if err != nil {
			return HeaderField{}, err
		}
	} else {
		// 索引名称
		field, err := d.getIndexedField(index)
		if err != nil {
			return HeaderField{}, err
		}
		name = field.Name
	}

	// 解码值
	value, err := d.decodeString(buf)
	if err != nil {
		return HeaderField{}, err
	}

	field := HeaderField{Name: name, Value: value}

	if addToTable {
		d.dynamicTable.Add(field)
	}

	return field, nil
}

// decodeInteger 解码整数
func (d *HPACKDecoder) decodeInteger(buf *bytes.Reader, prefixBits int) (uint64, error) {
	b, err := buf.ReadByte()
	if err != nil {
		return 0, err
	}

	mask := uint8((1 << prefixBits) - 1)
	value := uint64(b & mask)

	if value < uint64(mask) {
		return value, nil
	}

	// 多字节编码
	m := uint64(0)
	for {
		b, err := buf.ReadByte()
		if err != nil {
			return 0, err
		}

		value += uint64(b&0x7f) << m
		m += 7

		if b&0x80 == 0 {
			break
		}

		if m >= 63 {
			return 0, errors.New("integer overflow")
		}
	}

	return value, nil
}

// decodeString 解码字符串
func (d *HPACKDecoder) decodeString(buf *bytes.Reader) (string, error) {
	b, err := buf.ReadByte()
	if err != nil {
		return "", err
	}

	huffman := b&0x80 != 0
	buf.UnreadByte()

	length, err := d.decodeInteger(buf, 7)
	if err != nil {
		return "", err
	}

	if length == 0 {
		return "", nil
	}

	data := make([]byte, length)
	_, err = buf.Read(data)
	if err != nil {
		return "", err
	}

	if huffman {
		// 霍夫曼解码（简化实现）
		return string(data), nil
	}

	return string(data), nil
}

// getIndexedField 获取索引字段
func (d *HPACKDecoder) getIndexedField(index uint64) (HeaderField, error) {
	if index == 0 {
		return HeaderField{}, errors.New("invalid index 0")
	}

	if index < uint64(len(staticTable)) {
		return staticTable[index], nil
	}

	dynamicIndex := index - uint64(len(staticTable)) + 1
	if dynamicIndex > uint64(len(d.dynamicTable.entries)) {
		return HeaderField{}, fmt.Errorf("invalid dynamic table index: %d", dynamicIndex)
	}

	return d.dynamicTable.entries[dynamicIndex-1], nil
}

// Add 添加字段到动态表
func (dt *DynamicTable) Add(field HeaderField) {
	fieldSize := uint32(len(field.Name) + len(field.Value) + 32)

	// 确保有足够空间
	for dt.size+fieldSize > dt.maxSize && len(dt.entries) > 0 {
		dt.evict()
	}

	if fieldSize <= dt.maxSize {
		dt.entries = append([]HeaderField{field}, dt.entries...)
		dt.size += fieldSize
	}
}

// evict 驱逐最旧的条目
func (dt *DynamicTable) evict() {
	if len(dt.entries) == 0 {
		return
	}

	last := dt.entries[len(dt.entries)-1]
	dt.entries = dt.entries[:len(dt.entries)-1]
	dt.size -= uint32(len(last.Name) + len(last.Value) + 32)
}

// Size 计算头部字段大小
func (hf HeaderField) Size() uint32 {
	return uint32(len(hf.Name) + len(hf.Value) + 32)
}
