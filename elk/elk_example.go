package elk

import (
	"github.com/luxun9527/zlog"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// ELKExample 演示如何使用ELK功能
func ELKExample() {
	// 方法1: 通过配置文件初始化
	config := &zlog.Config{}

	// 使用viper读取配置
	v := viper.New()
	v.SetConfigFile("config.yaml")
	if err := v.ReadInConfig(); err != nil {
		panic(err)
	}

	// 使用mapstructure将配置映射到结构体
	if err := v.Unmarshal(config); err != nil {
		panic(err)
	}

	// 构建日志器
	logger := config.Build()
	defer logger.Sync()

	// 使用日志器
	logger.Info("这是一条发送到ELK的日志",
		zap.String("service", "example-service"),
		zap.String("version", "1.0.0"),
		zap.Time("timestamp", time.Now()),
	)

	logger.Error("这是一条错误日志",
		zap.String("error", "示例错误"),
		zap.String("module", "elk-example"),
	)
}

// ELKExampleDirect 演示如何直接配置ELK
func ELKExampleDirect() {
	// 方法2: 直接配置ELK
	config := &zlog.Config{
		Name:  "elk-example",
		Level: zap.NewAtomicLevelAt(zap.InfoLevel),
		Mode:  "console",
		Json:  true, // ELK需要JSON格式
		ELKConfig: &ELKConfig{
			Addresses: []string{"http://localhost:9200"},
			// 如果有认证需求，可以配置以下任一选项：
			// Username: "elastic",
			// Password: "password",
			// APIKey: "your-api-key",
			IndexPrefix: "myapp",
			FlushSec:    5,
			MaxCount:    100,
			BulkSize:    50,
			Async:       true,
		},
	}

	// 构建日志器
	logger := config.Build()
	defer logger.Sync()

	// 使用日志器
	sugar := logger.Sugar()

	// 结构化日志
	sugar.Infow("用户登录",
		"user_id", 12345,
		"username", "john_doe",
		"ip", "192.168.1.100",
		"user_agent", "Mozilla/5.0...",
		"timestamp", time.Now(),
	)

	// 错误日志
	sugar.Errorw("数据库连接失败",
		"error", "connection timeout",
		"database", "user_db",
		"retry_count", 3,
		"timestamp", time.Now(),
	)

	// 性能日志
	sugar.Infow("API请求完成",
		"method", "GET",
		"path", "/api/users",
		"status_code", 200,
		"response_time_ms", 150,
		"timestamp", time.Now(),
	)
}

// ELKExampleWithFields 演示使用zap.Field的高级用法
func ELKExampleWithFields() {
	config := &zlog.Config{
		Name:  "elk-advanced",
		Level: zap.NewAtomicLevelAt(zap.DebugLevel),
		Mode:  "console",
		Json:  true,
		ELKConfig: &ELKConfig{
			Addresses:   []string{"http://localhost:9200"},
			IndexPrefix: "advanced-app",
			FlushSec:    3,
			MaxCount:    50,
			Async:       true,
		},
	}

	logger := config.Build()
	defer logger.Sync()

	// 使用zap.Field进行结构化日志记录
	logger.Info("业务处理完成",
		zap.String("business_type", "order_processing"),
		zap.Int64("order_id", 987654321),
		zap.String("customer_id", "cust_12345"),
		zap.Float64("amount", 99.99),
		zap.String("currency", "USD"),
		zap.Duration("processing_time", 250*time.Millisecond),
		zap.Strings("tags", []string{"payment", "fulfillment", "notification"}),
		zap.Time("completed_at", time.Now()),
	)

	// 错误日志with堆栈
	logger.Error("处理订单时发生错误",
		zap.String("error_type", "payment_gateway_error"),
		zap.String("error_message", "insufficient funds"),
		zap.Int64("order_id", 987654321),
		zap.String("payment_method", "credit_card"),
		zap.Stack("stack_trace"),
	)
}
