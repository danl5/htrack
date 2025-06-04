package http2

import "fmt"

// SettingID SETTINGS参数ID
type SettingID uint16

const (
	SettingHeaderTableSize      SettingID = 0x1
	SettingEnablePush           SettingID = 0x2
	SettingMaxConcurrentStreams SettingID = 0x3
	SettingInitialWindowSize    SettingID = 0x4
	SettingMaxFrameSize         SettingID = 0x5
	SettingMaxHeaderListSize    SettingID = 0x6
)

// 默认设置值
const (
	DefaultHeaderTableSize      = 4096
	DefaultEnablePush           = 1
	DefaultMaxConcurrentStreams = 0 // 无限制
	DefaultInitialWindowSize    = 65535
	DefaultMaxFrameSize         = 16384
	DefaultMaxHeaderListSize    = 0 // 无限制
)

// 设置值限制
const (
	MinMaxFrameSize = 16384
	MaxMaxFrameSize = 16777215
	MaxWindowSize   = 2147483647
)

// String 返回设置ID的字符串表示
func (s SettingID) String() string {
	switch s {
	case SettingHeaderTableSize:
		return "HEADER_TABLE_SIZE"
	case SettingEnablePush:
		return "ENABLE_PUSH"
	case SettingMaxConcurrentStreams:
		return "MAX_CONCURRENT_STREAMS"
	case SettingInitialWindowSize:
		return "INITIAL_WINDOW_SIZE"
	case SettingMaxFrameSize:
		return "MAX_FRAME_SIZE"
	case SettingMaxHeaderListSize:
		return "MAX_HEADER_LIST_SIZE"
	default:
		return fmt.Sprintf("UNKNOWN_SETTING(%d)", uint16(s))
	}
}

// SettingsMap 设置映射
type SettingsMap map[SettingID]uint32

// NewDefaultSettings 创建默认设置
func NewDefaultSettings() SettingsMap {
	return SettingsMap{
		SettingHeaderTableSize:      DefaultHeaderTableSize,
		SettingEnablePush:           DefaultEnablePush,
		SettingMaxConcurrentStreams: DefaultMaxConcurrentStreams,
		SettingInitialWindowSize:    DefaultInitialWindowSize,
		SettingMaxFrameSize:         DefaultMaxFrameSize,
		SettingMaxHeaderListSize:    DefaultMaxHeaderListSize,
	}
}

// Get 获取设置值
func (s SettingsMap) Get(id SettingID) (uint32, bool) {
	val, ok := s[id]
	return val, ok
}

// Set 设置值
func (s SettingsMap) Set(id SettingID, val uint32) error {
	switch id {
	case SettingEnablePush:
		if val != 0 && val != 1 {
			return fmt.Errorf("invalid ENABLE_PUSH value: %d", val)
		}
	case SettingInitialWindowSize:
		if val > MaxWindowSize {
			return fmt.Errorf("invalid INITIAL_WINDOW_SIZE value: %d", val)
		}
	case SettingMaxFrameSize:
		if val < MinMaxFrameSize || val > MaxMaxFrameSize {
			return fmt.Errorf("invalid MAX_FRAME_SIZE value: %d", val)
		}
	}
	s[id] = val
	return nil
}