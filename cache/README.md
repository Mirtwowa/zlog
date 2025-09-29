# ZLog 缓存日志存储

本项目支持将日志存储到MySQL或Redis中，提供高效的日志查询和分析功能。

## 功能特性

- 🗄️ **多存储支持**: 支持MySQL和Redis两种存储方式
- ⚡ **高性能**: 批量写入、异步处理、连接池优化
- 🔍 **灵活查询**: 支持时间范围、级别、项目、关键词等多种查询条件
- 📊 **查询优化**: 内置查询缓存和性能监控
- 🔄 **多缓存写入**: 支持同时写入多个缓存系统
- 📈 **性能监控**: 提供缓存命中率、写入成功率等统计信息
- 🛡️ **错误处理**: 完善的错误处理和重试机制

## 快速开始

### 1. 配置文件方式

在 `config.yaml` 中配置缓存：

```yaml
cacheConfig:
  type: "mysql"  # 或 "redis"
  batch_size: 100
  flush_interval: "5s"
  max_cache_size: 10000
  async: true
  
  mysql:  # MySQL配置
    host: "localhost"
    port: 3306
    username: "root"
    password: "password"
    database: "zlog"
    charset: "utf8mb4"
    max_open_conns: 100
    max_idle_conns: 10
    table_prefix: "zlog"
    
  redis:  # Redis配置
    host: "localhost"
    port: 6379
    password: ""
    database: 0
    pool_size: 10
    key_prefix: "zlog"
```

### 2. 代码配置方式

```go
package main

import (
    "github.com/luxun9527/zlog"
    "github.com/luxun9527/zlog/cache"
    "go.uber.org/zap"
)

func main() {
    config := &zlog.Config{
        Name:  "my-service",
        Level: zap.NewAtomicLevelAt(zap.InfoLevel),
        Mode:  "console",
        Json:  true,
        CacheConfig: &cache.CacheConfig{
            Type:        "mysql",
            BatchSize:   100,
            FlushInterval: 5 * time.Second,
            MaxCacheSize: 10000,
            Async:       true,
            MySQL: &cache.MySQLConfig{
                Host:     "localhost",
                Port:     3306,
                Username: "root",
                Password: "password",
                Database: "zlog",
                TablePrefix: "zlog",
            },
        },
    }
    
    logger := config.Build()
    defer logger.Sync()
    
    // 使用日志器
    logger.Info("服务启动成功", 
        zap.String("version", "1.0.0"),
        zap.String("port", "8080"),
    )
}
```

## 存储方式

### MySQL存储

MySQL存储提供完整的SQL查询功能，适合复杂的日志分析。

#### 表结构

```sql
CREATE TABLE zlog_logs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    timestamp DATETIME(3) NOT NULL,
    level VARCHAR(10) NOT NULL,
    message TEXT NOT NULL,
    logger VARCHAR(100) DEFAULT '',
    caller VARCHAR(200) DEFAULT '',
    project VARCHAR(50) DEFAULT '',
    fields JSON DEFAULT NULL,
    created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),
    
    INDEX idx_timestamp (timestamp),
    INDEX idx_level (level),
    INDEX idx_project (project),
    INDEX idx_logger (logger),
    INDEX idx_created_at (created_at),
    INDEX idx_timestamp_level (timestamp, level),
    INDEX idx_project_timestamp (project, timestamp)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### 配置参数

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `host` | string | localhost | MySQL主机地址 |
| `port` | int | 3306 | MySQL端口 |
| `username` | string | - | 用户名 |
| `password` | string | - | 密码 |
| `database` | string | - | 数据库名 |
| `charset` | string | utf8mb4 | 字符集 |
| `max_open_conns` | int | 100 | 最大连接数 |
| `max_idle_conns` | int | 10 | 最大空闲连接数 |
| `table_prefix` | string | zlog | 表前缀 |

### Redis存储

Redis存储提供高速的键值查询，适合实时日志检索。

#### 数据结构

- **日志数据**: `zlog:log:{id}` - 存储完整的日志JSON
- **时间索引**: `zlog:index:timestamp:{date}` - 按日期分组的日志ID集合
- **级别索引**: `zlog:index:level:{level}` - 按级别分组的日志ID集合
- **项目索引**: `zlog:index:project:{project}` - 按项目分组的日志ID集合
- **日志器索引**: `zlog:index:logger:{logger}` - 按日志器分组的日志ID集合

#### 配置参数

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `host` | string | localhost | Redis主机地址 |
| `port` | int | 6379 | Redis端口 |
| `password` | string | "" | 密码（可选） |
| `database` | int | 0 | 数据库编号 |
| `pool_size` | int | 10 | 连接池大小 |
| `key_prefix` | string | zlog | 键前缀 |

## 查询功能

### 查询选项

```go
type QueryOptions struct {
    StartTime   *time.Time             // 开始时间
    EndTime     *time.Time             // 结束时间
    Levels      []string               // 日志级别
    Projects    []string               // 项目名称
    Loggers     []string               // 日志器名称
    Keywords    []string               // 关键词搜索
    Fields      map[string]interface{} // 字段过滤
    Limit       int                    // 限制数量
    Offset      int                    // 偏移量
    OrderBy     string                 // 排序字段
    OrderDir    string                 // 排序方向
}
```

### 查询示例

```go
// 1. 按时间范围查询
opts := &cache.QueryOptions{
    StartTime: &startTime,
    EndTime:   &endTime,
    Limit:     100,
    OrderBy:   "timestamp",
    OrderDir:  "desc",
}

// 2. 按级别查询
opts := &cache.QueryOptions{
    Levels:   []string{"error", "warn"},
    Limit:    50,
    OrderBy:  "timestamp",
    OrderDir: "desc",
}

// 3. 按项目查询
opts := &cache.QueryOptions{
    Projects: []string{"user-service", "order-service"},
    Limit:    100,
}

// 4. 关键词搜索
opts := &cache.QueryOptions{
    Keywords: []string{"登录", "失败", "错误"},
    Limit:    20,
}

// 5. 复合查询
opts := &cache.QueryOptions{
    StartTime: &startTime,
    EndTime:   &endTime,
    Levels:    []string{"error"},
    Projects:  []string{"user-service"},
    Keywords:  []string{"数据库"},
    Limit:     10,
    OrderBy:   "timestamp",
    OrderDir:  "desc",
}

// 执行查询
result, err := cache.Query(context.Background(), opts)
if err != nil {
    log.Printf("查询失败: %v", err)
    return
}

fmt.Printf("查询结果: 总数=%d, 当前页=%d, 大小=%d\n", 
    result.Total, result.Page, result.Size)
```

## 性能优化

### 1. 批量写入

```yaml
cacheConfig:
  batch_size: 100      # 批量大小，建议100-1000
  flush_interval: "5s" # 刷新间隔，建议5-30秒
  async: true          # 异步写入，推荐开启
```

### 2. 连接池配置

#### MySQL连接池
```yaml
mysql:
  max_open_conns: 100  # 最大连接数，根据并发量调整
  max_idle_conns: 10   # 最大空闲连接数
```

#### Redis连接池
```yaml
redis:
  pool_size: 10        # 连接池大小，建议10-50
```

### 3. 查询优化

```go
// 使用查询优化器
optimizer := cache.NewCacheQueryOptimizer(cache)

// 优化后的查询会自动缓存结果
result, err := optimizer.OptimizedQuery(ctx, opts)
```

### 4. 索引优化

MySQL会自动创建以下索引来优化查询性能：
- 时间戳索引
- 级别索引
- 项目索引
- 日志器索引
- 复合索引

## 监控和统计

### 缓存监控

```go
// 创建监控器
monitor := cache.NewCacheMonitor()

// 记录操作
monitor.RecordQuery(true)  // 记录缓存命中
monitor.RecordWrite(true)  // 记录写入成功

// 获取统计信息
stats := monitor.GetStats()
hitRate := monitor.GetCacheHitRate()

fmt.Printf("缓存命中率: %.2f%%\n", hitRate)
fmt.Printf("总查询数: %d\n", stats.TotalQueries)
fmt.Printf("总写入数: %d\n", stats.TotalWrites)
```

### 性能指标

- **缓存命中率**: 查询缓存的命中百分比
- **写入成功率**: 日志写入成功的百分比
- **平均查询时间**: 单次查询的平均耗时
- **平均写入时间**: 单次写入的平均耗时
- **连接池状态**: 当前连接数和空闲连接数

## 多缓存支持

支持同时写入多个缓存系统：

```go
// 创建多个缓存实例
mysqlCache, _ := cache.CreateCache(mysqlConfig)
redisCache, _ := cache.CreateCache(redisConfig)

// 创建多缓存写入器
multiWriter := cache.NewMultiCacheWriter(
    mysqlCache.(cache.CacheWriter),
    redisCache.(cache.CacheWriter),
)

// 同时写入到MySQL和Redis
entries := []cache.LogEntry{...}
multiWriter.WriteBatch(context.Background(), entries)
```

## 故障排除

### 1. 连接失败

**MySQL连接失败**:
- 检查主机地址和端口
- 验证用户名和密码
- 确认数据库存在
- 检查防火墙设置

**Redis连接失败**:
- 检查主机地址和端口
- 验证密码（如果设置了）
- 确认Redis服务运行状态

### 2. 性能问题

**写入慢**:
- 增加批量大小
- 启用异步写入
- 优化连接池配置
- 检查网络延迟

**查询慢**:
- 使用查询优化器
- 添加合适的索引
- 限制查询结果数量
- 优化查询条件

### 3. 内存使用高

- 减少批量大小
- 增加刷新频率
- 限制缓存大小
- 定期清理过期数据

## 最佳实践

1. **生产环境配置**:
   - 使用异步写入
   - 设置合适的批量大小
   - 配置连接池参数
   - 启用查询缓存

2. **索引策略**:
   - 根据查询模式创建索引
   - 定期分析查询性能
   - 清理不必要的索引

3. **监控告警**:
   - 设置缓存命中率告警
   - 监控写入失败率
   - 跟踪查询响应时间

4. **数据清理**:
   - 定期删除过期日志
   - 压缩历史数据
   - 备份重要日志

## 示例代码

完整的使用示例请参考：
- `example.go` - 基本使用示例
- `cache_test.go` - 测试用例
- 主项目中的集成示例

## 依赖版本

- MySQL Driver: v1.7.1
- Redis Client: v8.11.5
- Go: v1.20+
