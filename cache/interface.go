package cache

import (
	"context"
	"time"
)

// LogEntry 日志条目结构
type LogEntry struct {
	ID        int64                  `json:"id" db:"id"`
	Timestamp time.Time              `json:"timestamp" db:"timestamp"`
	Level     string                 `json:"level" db:"level"`
	Message   string                 `json:"message" db:"message"`
	Logger    string                 `json:"logger" db:"logger"`
	Caller    string                 `json:"caller" db:"caller"`
	Project   string                 `json:"project" db:"project"`
	Fields    map[string]interface{} `json:"fields" db:"fields"`
	CreatedAt time.Time              `json:"created_at" db:"created_at"`
}

// QueryOptions 查询选项
type QueryOptions struct {
	StartTime *time.Time             `json:"start_time,omitempty"`
	EndTime   *time.Time             `json:"end_time,omitempty"`
	Levels    []string               `json:"levels,omitempty"`
	Projects  []string               `json:"projects,omitempty"`
	Loggers   []string               `json:"loggers,omitempty"`
	Keywords  []string               `json:"keywords,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Limit     int                    `json:"limit,omitempty"`
	Offset    int                    `json:"offset,omitempty"`
	OrderBy   string                 `json:"order_by,omitempty"`  // timestamp, level, created_at
	OrderDir  string                 `json:"order_dir,omitempty"` // asc, desc
}

// QueryResult 查询结果
type QueryResult struct {
	Logs  []LogEntry `json:"logs"`
	Total int64      `json:"total"`
	Page  int        `json:"page"`
	Size  int        `json:"size"`
}

// CacheConfig 缓存配置
type CacheConfig struct {
	// 存储类型：mysql, redis
	Type string `json:"type" mapstructure:"type"`

	// MySQL配置
	MySQL *MySQLConfig `json:"mysql,omitempty" mapstructure:"mysql,omitempty"`

	// Redis配置
	Redis *RedisConfig `json:"redis,omitempty" mapstructure:"redis,omitempty"`

	// 通用配置
	BatchSize     int           `json:"batch_size" mapstructure:"batch_size"`         // 批量写入大小
	FlushInterval time.Duration `json:"flush_interval" mapstructure:"flush_interval"` // 刷新间隔
	MaxCacheSize  int           `json:"max_cache_size" mapstructure:"max_cache_size"` // 最大缓存大小
	Async         bool          `json:"async" mapstructure:"async"`                   // 是否异步写入
}

// MySQLConfig MySQL配置
type MySQLConfig struct {
	Host         string `json:"host" mapstructure:"host"`
	Port         int    `json:"port" mapstructure:"port"`
	Username     string `json:"username" mapstructure:"username"`
	Password     string `json:"password" mapstructure:"password"`
	Database     string `json:"database" mapstructure:"database"`
	Charset      string `json:"charset" mapstructure:"charset"`
	MaxOpenConns int    `json:"max_open_conns" mapstructure:"max_open_conns"`
	MaxIdleConns int    `json:"max_idle_conns" mapstructure:"max_idle_conns"`
	TablePrefix  string `json:"table_prefix" mapstructure:"table_prefix"`
}

// RedisConfig Redis配置
type RedisConfig struct {
	Host      string `json:"host" mapstructure:"host"`
	Port      int    `json:"port" mapstructure:"port"`
	Password  string `json:"password" mapstructure:"password"`
	Database  int    `json:"database" mapstructure:"database"`
	PoolSize  int    `json:"pool_size" mapstructure:"pool_size"`
	KeyPrefix string `json:"key_prefix" mapstructure:"key_prefix"`
}

// LogCache 日志缓存接口
type LogCache interface {
	// 写入日志条目
	WriteLogs(ctx context.Context, logs ...LogEntry) error

	// 批量写入日志
	WriteBatch(ctx context.Context, logs []LogEntry) error

	// 查询日志
	Query(ctx context.Context, opts *QueryOptions) (*QueryResult, error)

	// 根据ID查询日志
	GetByID(ctx context.Context, id int64) (*LogEntry, error)

	// 统计日志数量
	Count(ctx context.Context, opts *QueryOptions) (int64, error)

	// 删除过期日志
	DeleteExpired(ctx context.Context, before time.Time) error

	// 关闭连接
	Close() error

	// 健康检查
	Ping(ctx context.Context) error
}

// CacheWriter 缓存写入器接口，实现zapcore.WriteSyncer
type CacheWriter interface {
	LogCache
	// 实现zapcore.WriteSyncer接口
	Write(p []byte) (n int, err error)
	Sync() error
}
