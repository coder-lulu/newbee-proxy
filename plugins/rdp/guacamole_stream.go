package rdp

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	SocketTimeout  = 15 * time.Second
	MaxGuacMessage = 8192
)

// GuacamoleStream Guacamole数据流 - 基于mayfly-go的简化实现
type GuacamoleStream struct {
	conn net.Conn

	// ConnectionID Guacamole连接ID
	ConnectionID string
	timeout      time.Duration

	// 缓冲区管理
	buffer []rune
	reader *bufio.Reader
	writer *bufio.Writer
	logger logx.Logger
}

// GuacamoleConfiguration Guacamole配置
type GuacamoleConfiguration struct {
	ConnectionID        string
	Protocol            string
	Parameters          map[string]string
	OptimalScreenWidth  int
	OptimalScreenHeight int
	OptimalResolution   int
	AudioMimetypes      []string
	VideoMimetypes      []string
	ImageMimetypes      []string
}

// NewGuacamoleStream 创建新的Guacamole流
func NewGuacamoleStream(conn net.Conn, timeout time.Duration) *GuacamoleStream {
	return &GuacamoleStream{
		conn:    conn,
		timeout: timeout,
		buffer:  make([]rune, 0, MaxGuacMessage*3),
		reader:  bufio.NewReader(conn),
		writer:  bufio.NewWriter(conn),
		logger:  logx.WithContext(nil),
	}
}

// Write 向Guacamole发送消息
func (s *GuacamoleStream) Write(data []byte) (n int, err error) {
	if err = s.conn.SetWriteDeadline(time.Now().Add(s.timeout)); err != nil {
		return 0, err
	}

	n, err = s.writer.Write(data)
	if err != nil {
		return n, err
	}

	return n, s.writer.Flush()
}

// Available 返回是否有缓冲的消息
func (s *GuacamoleStream) Available() bool {
	return len(s.buffer) > 0
}

// Flush 重置内部缓冲区
func (s *GuacamoleStream) Flush() {
	s.buffer = s.buffer[:0]
}

// ReadSome 读取下一个完整的Guacamole指令 - 基于mayfly-go的实现
func (s *GuacamoleStream) ReadSome() ([]byte, error) {
	// 设置读取超时
	if err := s.conn.SetReadDeadline(time.Now().Add(s.timeout)); err != nil {
		return nil, fmt.Errorf("设置读取超时失败: %w", err)
	}

	for {
		// 检查缓冲区中是否有完整指令
		if instruction := s.parseNextInstruction(); instruction != nil {
			return []byte(string(instruction)), nil
		}

		// 从网络读取更多数据
		buffer := make([]byte, 4096)
		n, err := s.reader.Read(buffer)
		if err != nil {
			return nil, err
		}

		if n > 0 {
			// 添加到缓冲区
			runes := []rune(string(buffer[:n]))
			s.buffer = append(s.buffer, runes...)
		}
	}
}

// parseNextInstruction 解析下一个完整指令
func (s *GuacamoleStream) parseNextInstruction() []rune {
	for i := 0; i < len(s.buffer); i++ {
		if s.buffer[i] == ';' {
			// 找到完整指令
			instruction := s.buffer[:i+1]
			// 移除已解析的指令
			s.buffer = s.buffer[i+1:]
			return instruction
		}
	}
	return nil
}

// Close 关闭连接
func (s *GuacamoleStream) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// Handshake 执行Guacamole握手协议 - 基于mayfly-go的简化实现
func (s *GuacamoleStream) Handshake(config *GuacamoleConfiguration) error {
	s.logger.Infof("开始Guacamole握手，协议: %s", config.Protocol)

	// 1. 发送select指令
	selectArg := config.Protocol
	if config.ConnectionID != "" {
		selectArg = config.ConnectionID
	}

	selectInstruction := NewInstruction("select", selectArg)
	s.logger.Infof("发送select指令: %s", selectInstruction.String())

	if _, err := s.Write(selectInstruction.Byte()); err != nil {
		return fmt.Errorf("发送select指令失败: %w", err)
	}

	// 2. 接收args指令
	args, err := s.ReadAndParseInstruction("args")
	if err != nil {
		return fmt.Errorf("读取args指令失败: %w", err)
	}

	s.logger.Infof("收到args指令，参数数量: %d", len(args.Args))

	// 3. 构建参数值列表
	var argValues []string
	for _, argName := range args.Args {
		value := ""
		if config.Parameters != nil {
			if v, exists := config.Parameters[argName]; exists {
				value = v
			}
		}
		argValues = append(argValues, value)
	}

	// 4. 发送size指令
	sizeInstruction := NewInstruction("size",
		fmt.Sprintf("%d", config.OptimalScreenWidth),
		fmt.Sprintf("%d", config.OptimalScreenHeight),
		fmt.Sprintf("%d", config.OptimalResolution))

	s.logger.Infof("发送size指令: %s", sizeInstruction.String())
	if _, err := s.Write(sizeInstruction.Byte()); err != nil {
		return fmt.Errorf("发送size指令失败: %w", err)
	}

	// 5. 发送audio指令
	audioInstruction := NewInstruction("audio", config.AudioMimetypes...)
	s.logger.Infof("发送audio指令: %s", audioInstruction.String())
	if _, err := s.Write(audioInstruction.Byte()); err != nil {
		return fmt.Errorf("发送audio指令失败: %w", err)
	}

	// 6. 发送video指令
	videoInstruction := NewInstruction("video", config.VideoMimetypes...)
	s.logger.Infof("发送video指令: %s", videoInstruction.String())
	if _, err := s.Write(videoInstruction.Byte()); err != nil {
		return fmt.Errorf("发送video指令失败: %w", err)
	}

	// 7. 发送image指令
	imageInstruction := NewInstruction("image", config.ImageMimetypes...)
	s.logger.Infof("发送image指令: %s", imageInstruction.String())
	if _, err := s.Write(imageInstruction.Byte()); err != nil {
		return fmt.Errorf("发送image指令失败: %w", err)
	}

	// 8. 发送timezone指令
	timezoneInstruction := NewInstruction("timezone", "Asia/Shanghai")
	s.logger.Infof("发送timezone指令: %s", timezoneInstruction.String())
	if _, err := s.Write(timezoneInstruction.Byte()); err != nil {
		return fmt.Errorf("发送timezone指令失败: %w", err)
	}

	// 9. 发送connect指令
	connectInstruction := NewInstruction("connect", argValues...)
	s.logger.Infof("发送connect指令: %s", connectInstruction.String())
	if _, err := s.Write(connectInstruction.Byte()); err != nil {
		return fmt.Errorf("发送connect指令失败: %w", err)
	}

	// 10. 接收ready指令
	ready, err := s.ReadAndParseInstruction("ready")
	if err != nil {
		return fmt.Errorf("读取ready指令失败: %w", err)
	}

	if len(ready.Args) == 0 {
		return fmt.Errorf("未收到连接ID")
	}

	s.ConnectionID = ready.Args[0]
	s.Flush() // 清理缓冲区

	s.logger.Infof("Guacamole握手成功，连接ID: %s", s.ConnectionID)
	return nil
}

// ReadAndParseInstruction 读取并解析指定操作码的指令
func (s *GuacamoleStream) ReadAndParseInstruction(expectedOpcode string) (*Instruction, error) {
	data, err := s.ReadSome()
	if err != nil {
		return nil, err
	}

	instruction, err := ParseInstruction(string(data))
	if err != nil {
		return nil, err
	}

	if instruction.Opcode != expectedOpcode {
		return nil, fmt.Errorf("期望指令 %s，但收到 %s", expectedOpcode, instruction.Opcode)
	}

	return instruction, nil
}

// NewGuacamoleConfiguration 创建新的Guacamole配置
func NewGuacamoleConfiguration() *GuacamoleConfiguration {
	return &GuacamoleConfiguration{
		Parameters:          make(map[string]string),
		OptimalScreenWidth:  1024,
		OptimalScreenHeight: 768,
		OptimalResolution:   96,
		AudioMimetypes:      []string{"audio/L16", "rate=44100", "channels=2"},
		VideoMimetypes:      []string{},
		ImageMimetypes:      []string{"image/jpeg", "image/png", "image/webp"},
	}
}

// Instruction Guacamole指令结构
type Instruction struct {
	Opcode string
	Args   []string
}

// NewInstruction 创建新指令
func NewInstruction(opcode string, args ...string) *Instruction {
	return &Instruction{
		Opcode: opcode,
		Args:   args,
	}
}

// String 返回指令的字符串表示
func (i *Instruction) String() string {
	return BuildInstruction(i.Opcode, i.Args...)
}

// Byte 返回指令的字节表示
func (i *Instruction) Byte() []byte {
	return []byte(i.String())
}

// ParseInstruction 解析Guacamole指令字符串
func ParseInstruction(data string) (*Instruction, error) {
	// 移除末尾分号
	data = strings.TrimSuffix(data, ";")

	// 按逗号分割
	parts := strings.Split(data, ",")
	if len(parts) == 0 {
		return nil, fmt.Errorf("空指令")
	}

	// 解析操作码
	firstPart := parts[0]
	if dotIndex := strings.Index(firstPart, "."); dotIndex != -1 {
		opcode := firstPart[dotIndex+1:]

		// 解析参数
		var args []string
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			if dotIndex := strings.Index(part, "."); dotIndex != -1 {
				arg := part[dotIndex+1:]
				args = append(args, arg)
			}
		}

		return &Instruction{
			Opcode: opcode,
			Args:   args,
		}, nil
	}

	return nil, fmt.Errorf("无效的指令格式: %s", data)
}

// BuildInstruction 构建Guacamole指令字符串
func BuildInstruction(opcode string, args ...string) string {
	var parts []string

	// 添加操作码
	parts = append(parts, fmt.Sprintf("%d.%s", len(opcode), opcode))

	// 添加参数
	for _, arg := range args {
		parts = append(parts, fmt.Sprintf("%d.%s", len(arg), arg))
	}

	return strings.Join(parts, ",") + ";"
}

// InstructionReader 指令读取器接口
type InstructionReader interface {
	ReadSome() ([]byte, error)
	Available() bool
	Flush()
}

// 确保GuacamoleStream实现了InstructionReader接口
var _ InstructionReader = (*GuacamoleStream)(nil)
