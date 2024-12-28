package zlog

import (
	"context"
	"go.uber.org/zap"
)

var (
	RedisLogger *redisLogger
)

func init() {
	//初始化全局变量RedisLogger
	RedisLogger = &redisLogger{
		logger: DefaultLogger.With(zap.String("module", RedisModuleKey)).Sugar(),
	}
	//使用全局的 DefaultLogger，为 Redis 日志添加一个模块标识字段 module，值为 redis。
	//调用了 zap.Logger.With 方法，为所有日志条目动态添加 module: redis 键值对。
	//通过 Sugar 方法将 zap.Logger 转为 zap.SugaredLogger，支持格式化输出。
}

// 它是一个专门用于记录 Redis 日志的封装组件，提供方法打印格式化日志和动态更新日志实例。
type redisLogger struct {
	logger *zap.SugaredLogger
}

func (rl *redisLogger) Printf(ctx context.Context, format string, v ...interface{}) {
	rl.logger.Infof(format, v...)
}

// Update 功能：
//
// 动态更新 redisLogger 使用的 logger 实例。
// 如果没有传入新的 zap.Logger 实例，则使用全局的 DefaultLogger 重新初始化。
// 参数：
//
// logger：变长参数，接受一个或多个新的 zap.Logger 实例。
// 如果为空，回退为默认配置。
// 如果提供了新的实例，则切换到该实例。
// 工作逻辑：
//
// 当用户需要改变 Redis 日志的输出配置（如日志级别或输出目标）时，调用 Update 方法动态替换 logger 实例。
// Sugar 方法将 zap.Logger 转换为 zap.SugaredLogger，便于使用格式化日志。
func (rl *redisLogger) Update(logger ...*zap.Logger) {
	if len(logger) == 0 {
		rl.logger = DefaultLogger.With(zap.String("module", RedisModuleKey)).Sugar()
		return
	}
	rl.logger = logger[0].Sugar()
}
