# ZLog ELK 支持

本项目现已支持将日志直接发送到 Elasticsearch，实现 ELK 日志收集和分析。

## 功能特性

- 🚀 **高性能**: 支持批量发送和异步处理
- 🔒 **安全认证**: 支持用户名/密码、API密钥、服务令牌等多种认证方式
- 📊 **灵活配置**: 支持自定义索引前缀、刷新间隔、批量大小等
- 🎯 **自动索引**: 按日期自动创建索引（如：zlog-2024.01.15）
- 📝 **结构化日志**: 强制使用JSON格式，便于Elasticsearch解析
- ⚡ **实时监控**: 支持实时日志级别调整

## 快速开始

### 1. 配置文件方式

在 `config.yaml` 中添加 ELK 配置：

```yaml
level: info
mode: console
json: true  # ELK需要JSON格式

elkConfig:
  addresses:
    - "http://localhost:9200"
  username: ""           # 可选
  password: ""           # 可选
  apiKey: ""             # 可选
  serviceToken: ""       # 可选
  indexPrefix: "myapp"   # 索引前缀，默认zlog
  flushSec: 5            # 刷新间隔(秒)
  maxCount: 100          # 最大缓存数量
  bulkSize: 100          # 批量大小
  async: true            # 异步发送
```

### 2. 代码配置方式

```go
package main

import (
    "github.com/luxun9527/zlog"
    "go.uber.org/zap"
)

func main() {
    config := &zlog.Config{
        Name:  "my-service",
        Level: zap.NewAtomicLevelAt(zap.InfoLevel),
        Mode:  "console",
        Json:  true,
        ELKConfig: &zlog.ELKConfig{
            Addresses: []string{"http://localhost:9200"},
            IndexPrefix: "myapp",
            FlushSec: 5,
            MaxCount: 100,
            Async: true,
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

## 配置参数详解

### ELKConfig 结构体

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `addresses` | []string | ["http://localhost:9200"] | Elasticsearch地址列表 |
| `username` | string | "" | 用户名（可选） |
| `password` | string | "" | 密码（可选） |
| `apiKey` | string | "" | API密钥（可选） |
| `serviceToken` | string | "" | 服务令牌（可选） |
| `indexPrefix` | string | "zlog" | 索引前缀 |
| `flushSec` | int64 | 5 | 刷新间隔（秒） |
| `maxCount` | int64 | 100 | 最大缓存数量 |
| `bulkSize` | int | 100 | 批量发送大小 |
| `async` | bool | true | 是否异步发送 |

## 认证方式

### 1. 用户名/密码认证

```yaml
elkConfig:
  addresses:
    - "http://localhost:9200"
  username: "elastic"
  password: "your_password"
```

### 2. API密钥认证

```yaml
elkConfig:
  addresses:
    - "http://localhost:9200"
  apiKey: "your_api_key"
```

### 3. 服务令牌认证

```yaml
elkConfig:
  addresses:
    - "http://localhost:9200"
  serviceToken: "your_service_token"
```

## 索引命名规则

索引按以下规则自动创建：

- 格式：`{indexPrefix}-{YYYY.MM.DD}`
- 示例：`myapp-2024.01.15`

## 日志字段结构

发送到 Elasticsearch 的日志包含以下标准字段：

```json
{
  "@timestamp": "2024-01-15T10:30:00Z",
  "level": "info",
  "msg": "用户登录成功",
  "logger": "main",
  "caller": "main.go:25",
  "project": "myapp",
  "user_id": 12345,
  "username": "john_doe"
}
```

## 性能优化建议

### 1. 批量配置

```yaml
elkConfig:
  flushSec: 10      # 生产环境建议10-30秒
  maxCount: 500     # 生产环境建议500-1000
  bulkSize: 500     # 与maxCount保持一致
```

### 2. 异步发送

```yaml
elkConfig:
  async: true  # 推荐使用异步发送
```

### 3. 索引生命周期管理

建议在 Elasticsearch 中配置索引生命周期策略：

```json
{
  "policy": {
    "phases": {
      "hot": {
        "actions": {
          "rollover": {
            "max_size": "50GB",
            "max_age": "7d"
          }
        }
      },
      "delete": {
        "min_age": "30d"
      }
    }
  }
}
```

## 故障排除

### 1. 连接失败

检查 Elasticsearch 地址和端口是否正确：

```bash
curl -X GET "localhost:9200/"
```

### 2. 认证失败

验证用户名/密码或API密钥是否正确：

```bash
curl -u elastic:password -X GET "localhost:9200/"
```

### 3. 索引权限

确保有创建和管理索引的权限。

### 4. 网络问题

检查防火墙设置和网络连通性。

## 监控和调试

### 1. 启用调试日志

```go
config := &zlog.Config{
    Level: zap.NewAtomicLevelAt(zap.DebugLevel),
    // ... 其他配置
}
```

### 2. 检查发送状态

在应用日志中查看 ELK 相关的错误信息。

### 3. Elasticsearch 监控

使用 Elasticsearch 的监控API检查索引状态：

```bash
curl -X GET "localhost:9200/_cat/indices?v"
```

## 最佳实践

1. **结构化日志**: 使用 zap.Field 记录结构化数据
2. **合理设置级别**: 生产环境避免使用 Debug 级别
3. **索引管理**: 定期清理旧索引，避免存储空间不足
4. **监控告警**: 设置日志发送失败的告警机制
5. **性能调优**: 根据日志量调整批量参数

## 示例代码

完整的使用示例请参考 `elk_example.go` 文件。

## 依赖版本

- Elasticsearch: v8.x
- Go Elasticsearch Client: v8.12.0
