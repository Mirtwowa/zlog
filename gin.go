package zlog

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap/zapcore"
	"time"
)

var (
	GinOutPut *LoggerWriter // 声明一个全局变量 GinOutPut，类型为自定义的 LoggerWriter
)

func init() {
	// 初始化 GinOutPut，默认使用 Debug 日志级别和 DefaultLogger（假设已经在其他文件定义）
	GinOutPut = NewWriter(DefaultLogger, zapcore.DebugLevel)
}
func GetGinLogger(conf ...gin.LoggerConfig) gin.HandlerFunc {
	if len(conf) == 0 {
		// 如果未传入配置参数，使用默认配置创建 Gin 日志中间件
		return gin.LoggerWithConfig(gin.LoggerConfig{
			Formatter: LogFormatter, // 自定义日志格式化器
			Output:    GinOutPut,    // 将日志输出到 GinOutPut
		})
	}
	// 如果传入了自定义配置参数，使用传入的配置
	return gin.LoggerWithConfig(conf[0])
}

var LogFormatter = func(param gin.LogFormatterParams) string {
	// 如果请求耗时超过一分钟，进行秒级截断
	if param.Latency > time.Minute {
		param.Latency = param.Latency.Truncate(time.Second)
	}
	// 格式化日志内容，包含状态码、请求路径、方法、耗时和客户端 IP
	data := fmt.Sprintf(
		"[GIN] statusCode:%v path:%s method:%s cost:%v clientIp:%s",
		param.StatusCode, param.Path, param.Method, param.Latency, param.ClientIP,
	)
	return data
}
