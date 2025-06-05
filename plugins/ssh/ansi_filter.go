package ssh

import (
	"regexp"
	"strings"
)

// ANSIFilter ANSI控制序列过滤器
type ANSIFilter struct {
	removeTitle       bool // 是否移除窗口标题设置
	removeBracketMode bool // 是否移除bracketed paste mode
	removeColors      bool // 是否移除颜色控制
}

// NewANSIFilter 创建新的ANSI过滤器
func NewANSIFilter(removeTitle, removeBracketMode, removeColors bool) *ANSIFilter {
	return &ANSIFilter{
		removeTitle:       removeTitle,
		removeBracketMode: removeBracketMode,
		removeColors:      removeColors,
	}
}

// FilterANSISequences 过滤ANSI控制序列
func (f *ANSIFilter) FilterANSISequences(data []byte) []byte {
	text := string(data)

	if f.removeTitle {
		// 移除窗口标题设置序列 ]0;...BEL 或 ]0;...ST
		titleRegex := regexp.MustCompile(`\]0;[^\x07\x1b]*(\x07|\x1b\\)`)
		text = titleRegex.ReplaceAllString(text, "")
	}

	if f.removeBracketMode {
		// 移除bracketed paste mode控制
		text = strings.ReplaceAll(text, "\x1b[?2004h", "") // 启用
		text = strings.ReplaceAll(text, "\x1b[?2004l", "") // 禁用
	}

	if f.removeColors {
		// 移除颜色和样式控制序列
		colorRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
		text = colorRegex.ReplaceAllString(text, "")
	}

	return []byte(text)
}

// Common ANSI sequences for reference:
// ESC]0;titleSTRING_TERMINATOR  - Set window title
// ESC[?2004h                    - Enable bracketed paste mode
// ESC[?2004l                    - Disable bracketed paste mode
// ESC[0m                        - Reset all attributes
// ESC[1m                        - Bold
// ESC[31m                       - Red foreground
// ESC[42m                       - Green background
