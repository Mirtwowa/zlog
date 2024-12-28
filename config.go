package zlog

import (
	"fmt"
	"github.com/luxun9527/zlog/report"  // 自定义的日志上报模块
	"github.com/mitchellh/mapstructure" // 用于将 map 转换为结构体
	"go.uber.org/zap"                   // 高性能的日志库
	"go.uber.org/zap/zapcore"           // zap 的核心模块
	"gopkg.in/natefinch/lumberjack.v2"  // 用于日志文件分割
	"log"
	"net/http" // 用于启动 HTTP 服务
	"os"
	"reflect" // 用于反射类型检查
	"sync"    // 用于并发安全
	"time"    // 时间相关功能
)

const (
	_defaultBufferSize    = 256 * 1024       // 缓存区大小，默认 256 KB
	_defaultFlushInterval = 30 * time.Second // 异步日志的默认刷新间隔
)

const (
	_file    = "file"    // 文件模式
	_console = "console" // 控制台模式
)

var (
	_once sync.Once // 确保日志服务器只初始化一次
)

// 日志配置结构体
type Config struct {
	Name          string               `json:",optional" mapstructure:"name"`           // 日志项目名称
	Level         zap.AtomicLevel      `json:"Level" mapstructure:"level"`              // 日志级别
	Stacktrace    bool                 `json:",default=true" mapstructure:"stacktrace"` // 是否显示堆栈
	AddCaller     bool                 `json:",default=true" mapstructure:"addCaller"`  // 是否显示调用者信息
	CallerShip    int                  `json:",default=3" mapstructure:"callerShip"`    // 调用链级别
	Mode          string               `json:",default=console" mapstructure:"mode"`    // 输出模式，console 或 file
	FileName      string               `json:",optional" mapstructure:"filename"`       // 日志文件名
	ErrorFileName string               `json:",optional" mapstructure:"errorFileName"`  // 错误日志文件名
	MaxSize       int                  `json:",optional" mapstructure:"maxSize"`        // 日志文件最大大小 (MB)
	MaxAge        int                  `json:",optional" mapstructure:"maxAge"`         // 日志保留天数
	MaxBackup     int                  `json:",optional" mapstructure:"maxBackUp"`      // 日志最大备份数
	Async         bool                 `json:",optional" mapstructure:"async"`          // 是否异步日志
	Json          bool                 `json:",optional" mapstructure:"json"`           // 是否输出 JSON 格式
	Compress      bool                 `json:",optional" mapstructure:"compress"`       // 是否压缩日志
	Console       bool                 `json:"console" mapstructure:"console"`          // 是否在 file 模式下同时输出到控制台
	Color         bool                 `json:",default=true" mapstructure:"color"`      // 非 JSON 格式下是否添加颜色
	Port          int32                `json:",default=true" mapstructure:"port"`       // 启动日志 HTTP 服务的端口
	ReportConfig  *report.ReportConfig `json:",optional" mapstructure:"reportConfig"`   // 日志上报配置
	options       []zap.Option         // zap 选项
}

// 更新日志级别
func (lc *Config) UpdateLevel(level zapcore.Level) {
	lc.Level.SetLevel(level)
}

// 构建日志对象
func (lc *Config) Build() *zap.Logger {
	if lc.Mode != _file && lc.Mode != _console {
		log.Panicln("mode must be console or file") // 模式只能是 console 或 file
	}
	if lc.Mode == _file && lc.FileName == "" {
		log.Panicln("file mode, but file name is empty") // 如果是文件模式，必须提供文件名
	}

	var (
		ws      zapcore.WriteSyncer // 普通日志写入器
		errorWs zapcore.WriteSyncer // 错误日志写入器
		encoder zapcore.Encoder     // 编码器
	)

	// 日志编码配置
	encoderConfig := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		TimeKey:        "time",
		NameKey:        "logger",
		CallerKey:      "caller",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder, // 小写日志级别
		EncodeTime:     CustomTimeEncoder,             // 自定义时间格式
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.FullCallerEncoder,
	}

	// 配置写入器
	if lc.Mode == _console {
		ws = zapcore.Lock(os.Stdout) // 输出到控制台
	} else {
		normalConfig := &lumberjack.Logger{ // 日志文件分割配置
			Filename:   lc.FileName,
			MaxSize:    lc.MaxSize,
			MaxAge:     lc.MaxAge,
			MaxBackups: lc.MaxBackup,
			LocalTime:  true,
			Compress:   lc.Compress,
		}
		if lc.ErrorFileName != "" { // 配置错误日志文件
			errorConfig := &lumberjack.Logger{
				Filename:   lc.ErrorFileName,
				MaxSize:    lc.MaxSize,
				MaxAge:     lc.MaxAge,
				MaxBackups: lc.MaxBackup,
				LocalTime:  true,
				Compress:   lc.Compress,
			}
			errorWs = zapcore.Lock(zapcore.AddSync(errorConfig))
		}
		ws = zapcore.Lock(zapcore.AddSync(normalConfig))
	}

	// 非 JSON 格式是否加颜色
	if lc.Color && !lc.Json {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder // 加颜色
	}
	if lc.Json {
		encoder = zapcore.NewJSONEncoder(encoderConfig) // JSON 格式
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderConfig) // 控制台格式
	}

	// 如果是异步日志
	if lc.Async {
		ws = &zapcore.BufferedWriteSyncer{
			WS:            ws,
			Size:          _defaultBufferSize,
			FlushInterval: _defaultFlushInterval,
		}
		if errorWs != nil {
			errorWs = &zapcore.BufferedWriteSyncer{
				WS:            errorWs,
				Size:          _defaultBufferSize,
				FlushInterval: _defaultFlushInterval,
			}
		}
	}

	var cores = []zapcore.Core{zapcore.NewCore(encoder, ws, lc.Level)}
	if errorWs != nil {
		highCore := zapcore.NewCore(encoder, errorWs, zapcore.ErrorLevel)
		cores = append(cores, highCore)
	}

	// 文件模式输出到控制台
	if lc.Mode == _file && lc.Console {
		consoleCore := zapcore.NewCore(encoder, zapcore.Lock(os.Stdout), lc.Level)
		cores = append(cores, consoleCore)
	}

	// 如果启用了日志上报
	if lc.ReportConfig != nil {
		if !lc.Json { // 日志上报强制 JSON 格式
			encoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
			encoder = zapcore.NewJSONEncoder(encoderConfig)
		}
		if lc.ReportConfig.Level == (zap.AtomicLevel{}) {
			lc.ReportConfig.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
		}
		reportCore := zapcore.NewCore(encoder, report.NewReportWriterBuffer(lc.ReportConfig), lc.ReportConfig.Level)
		cores = append(cores, reportCore)
	}

	core := zapcore.NewTee(cores...) // 合并日志核心
	logger := zap.New(core)

	// 添加调用者信息
	if lc.AddCaller {
		lc.options = append(lc.options, zap.AddCaller())
		if lc.CallerShip != 0 {
			lc.options = append(lc.options, zap.AddCallerSkip(lc.CallerShip))
		}
	}

	// 是否添加堆栈信息
	if lc.Stacktrace {
		lc.options = append(lc.options, zap.AddStacktrace(zap.PanicLevel))
	}

	// 设置项目名
	if lc.Name != "" {
		logger = logger.With(zap.String("project", lc.Name))
	}

	logger = logger.WithOptions(lc.options...) // 应用选项

	// 启动日志 HTTP 服务
	if lc.Port > 0 {
		lc.InitLogServer(lc.Port)
		logger.Sugar().Infof("log server init success, port:%d", lc.Port)
	}

	return logger
}

// 初始化日志 HTTP 服务
func (lc *Config) InitLogServer(port int32) {
	go func(p int32) {
		_once.Do(func() { // 确保只初始化一次
			if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", p), lc.Level); err != nil {
				zap.S().Error("init log server start failed", zap.Error(err))
			}
		})
	}(port)
}

func CustomTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02-15:04:05"))
}

func StringToLogLevelHookFunc() mapstructure.DecodeHookFunc {
	return func(
		f reflect.Type, // 源数据的类型
		t reflect.Type, // 目标数据的类型
		data interface{}) (interface{}, error) {
		// 检查源类型是否为字符串
		if f.Kind() != reflect.String {
			return data, nil
		}
		// 尝试将字符串解析为 zap 的日志级别
		atomicLevel, err := zap.ParseAtomicLevel(data.(string))
		if err != nil {
			// 如果解析失败，返回原始数据
			return data, nil
		}
		// 成功转换为日志级别
		return atomicLevel, nil
	}
}
