package zlog

import (
	"fmt"
	"github.com/zeromicro/go-zero/core/logx"
	"go.uber.org/zap"
)

type Field = logx.LogField //定义别名
// ErrorField 记录错误信息
func ErrorField(err error) Field {
	return logx.Field("error", err)
}

// DataField 记录错误数据
func DataField(data interface{}) Field {
	return logx.Field("data", data)
}

// ParamField 记录输入参数
func ParamField(data interface{}) Field {
	return logx.Field("param", data)
}

type ZapWriter struct {
	logger *zap.Logger //封装了 Zap 的日志写入器，适配 logx.Writer 接口。
}

// NewZapWriter 工厂方法，用于创建和实例化
func NewZapWriter(logger *zap.Logger) logx.Writer {
	return &ZapWriter{
		logger: logger,
	}
}

// Alert ZapWriter实现接口
func (w *ZapWriter) Alert(v interface{}) {
	w.logger.Error(fmt.Sprint(v))
}

// Close 在日志系统关闭时，调用 zap.Logger 的 Sync 方法，确保所有日志已写入输出。
// zap.Logger.Sync 是 Zap 提供的同步方法，用于清空缓存。
func (w *ZapWriter) Close() error {
	return w.logger.Sync()
}

// Debug 记录调试级别的日志信息。
// 将 go-zero 的日志字段通过 toZapFields 转换为 Zap 的字段格式。
func (w *ZapWriter) Debug(v interface{}, fields ...logx.LogField) {
	w.logger.Debug(fmt.Sprint(v), toZapFields(fields...)...)
}

func (w *ZapWriter) Error(v interface{}, fields ...logx.LogField) {
	w.logger.Error(fmt.Sprint(v), toZapFields(fields...)...)
}

func (w *ZapWriter) Info(v interface{}, fields ...logx.LogField) {
	w.logger.Info(fmt.Sprint(v), toZapFields(fields...)...)
}

func (w *ZapWriter) Severe(v interface{}) {
	w.logger.Panic(fmt.Sprint(v))
}

// Severef writes v with format into severe log.

// Slow 记录慢日志，使用 Warn 级别。
func (w *ZapWriter) Slow(v interface{}, fields ...logx.LogField) {
	w.logger.Warn(fmt.Sprint(v), toZapFields(fields...)...)
}

// Stack 记录带堆栈信息的错误日志。
// zap.Stack("stack") 添加了当前调用栈信息。
func (w *ZapWriter) Stack(v interface{}) {
	w.logger.Error(fmt.Sprint(v), zap.Stack("stack"))
}

// Stat 记录统计日志
func (w *ZapWriter) Stat(v interface{}, fields ...logx.LogField) {
	w.logger.Info(fmt.Sprint(v), toZapFields(fields...)...)
}

// 将 logx.LogField 转换为 Zap 的字段类型 zap.Field。
// 遍历 fields 参数并将每个字段转换为 zap.Any 类型字段。
func toZapFields(fields ...logx.LogField) []zap.Field {
	zapFields := make([]zap.Field, 0, len(fields))
	for _, f := range fields {
		zapFields = append(zapFields, zap.Any(f.Key, f.Value))
	}
	return zapFields
}
