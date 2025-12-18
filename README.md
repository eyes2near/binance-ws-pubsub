# binance-ws-pubsub

一个轻量的 WebSocket Pub/Sub Broker + Binance 数据 Publisher + 终端 Subscriber。

- **broker**: WebSocket 服务器，负责 topic 订阅与事件广播。
- **publisher**: 从 Binance（Spot / COIN-M）拉取数据并发布到 broker。
  - 支持发布 **订单簿 band 快照**。
  - 支持发布 **trade 流**。
- **subscriber**: 连接 broker 订阅 topic，在终端原地刷新显示。
  - 支持显示 **订单簿 band 快照**。
  - 支持显示 **trade 流（每 1 秒聚合/刷新一次）**。

---

## 先决条件

- Go 工具链 (推荐 Go 1.20+)。
- 能访问 Binance 网络 (publisher 需要)。

---

## 推荐运行顺序

1.  启动 **broker** (WebSocket 服务器)。
2.  启动 **publisher** (从 Binance 拉数据并 publish 到 broker)。
3.  启动 **subscriber** (subscribe topic 并显示)。

---

## 运行组件 (Windows PowerShell)

在 **不同的 PowerShell 窗口** 中按顺序执行以下命令。

### 1. 启动 Broker

Broker 默认监听 `ws://localhost:8080/ws`。

```powershell
go run ./cmd/broker
```

如需修改监听地址，请使用 `-listen` 参数。查看帮助获取更多信息：

```powershell
go run ./cmd/broker -h
```

### 2. 启动 Publisher

Publisher 默认连接 Broker (`ws://localhost:8080/ws`)。

#### Topic 约定

Publisher 会根据市场和数据类型生成 Topic，格式如下：
- **订单簿 band**: `orderbook.band.{SYMBOL}`
- **Trades**: `trades.{SYMBOL}`

`{SYMBOL}` 的具体内容取决于市场：
- **Spot**: 输入的交易对 (e.g., `BTCUSDT`)。
- **COIN-M**: 合约符号 (e.g., `BTCUSD_231229`)，由 publisher 自动解析。

**建议**: 启动 publisher 后，直接从其控制台输出中复制 topic 字符串给 subscriber 使用。

#### 市场模式

**A) 现货 (Spot) - 默认模式**

```powershell
# 只发布 BTCUSDT 的订单簿 band (默认行为)
go run ./cmd/publisher -market spot -symbol BTCUSDT

# 同时发布 orderbook 和 trades
go run ./cmd/publisher -market spot -symbol BTCUSDT -trades=true

# 只发布 trades
go run ./cmd/publisher -market spot -symbol BTCUSDT -orderbook=false -trades=true
```

**B) 币本位交割 (COIN-M)**

使用 `-market coin-m`，并指定 `-symbol` (基础币种) 和 `-contract` (`current` 或 `next`)。

```powershell
# 发布 BTC/USD 当季合约的订单簿 band
go run ./cmd/publisher -market coin-m -symbol BTC -contract current

# 发布 ETH/USD 次季合约的订单簿 band
go run ./cmd/publisher -market coin-m -symbol ETH -contract next

# 同时发布 COIN-M 合约的 orderbook 和 trades
go run ./cmd/publisher -market coin-m -symbol BTC -contract current -trades=true
```

#### Publisher 常用参数

```powershell
go run ./cmd/publisher -h
```
- `-broker`: Broker 的 WebSocket 地址。
- `-market`: `spot` / `coin-m`。
- `-symbol`: 市场对应的交易对或基础币种。
- `-contract`: `current` / `next` (仅用于 coin-m)。
- `-orderbook`: 是否发布 orderbook (默认 `true`)。
- `-trades`: 是否发布 trades (默认 `false`)。

### 3. 启动 Subscriber

Subscriber 可以同时订阅订单簿和 trades topic。UI 每秒刷新一次，trades 数据会按秒聚合，以避免信息滚动过快。

#### 订阅示例

```powershell
# 只订阅订单簿
go run ./cmd/subscriber -topic orderbook.band.BTCUSDT

# 同时订阅订单簿和 trades (推荐)
go run ./cmd/subscriber -topic orderbook.band.BTCUSDT -trades-topic trades.BTCUSDT

# 只订阅 trades
go run ./cmd/subscriber -topic "" -trades-topic trades.BTCUSDT

# 订阅 COIN-M 合约 (SYMBOL 从 publisher 获取)
go run ./cmd/subscriber -topic orderbook.band.BTCUSD_231229 -trades-topic trades.BTCUSD_231229
```

#### Subscriber 常用参数

```powershell
go run ./cmd/subscriber -h
```
- `-broker`: Broker 的 WebSocket 地址。
- `-topic`: 订单簿 topic (可为空)。
- `-trades-topic`: Trades topic (可为空)。
- `-rows`: 订单簿显示的行数 (默认 20)。
- `-interval`: UI 刷新间隔 (默认 `1s`)。

---

## 注意事项

- **平滑退出**: 在任何组件的窗口中按 `Ctrl+C` 即可平滑退出。
- **显示问题**: 如果 subscriber 不在原地刷新而是持续向下滚动，可能是终端对 ANSI/VT 控制符的支持不佳。本项目已针对此问题优化，在 Windows Terminal, PowerShell, VSCode Terminal 中通常表现良好。

---

## 后台运行 (可选)

你可以在后台启动所有组件。首先，请在 PowerShell 中进入项目根目录：

```powershell
cd {project_home}
```
*将 `{project_home}` 替换为你的项目实际路径。*

然后执行以下命令：

```powershell
# 启动 Broker
Start-Process powershell -ArgumentList '-NoExit', '-Command', 'go run ./cmd/broker'

# 启动 Publisher (以 Spot 市场为例)
Start-Process powershell -ArgumentList '-NoExit', '-Command', 'go run ./cmd/publisher -market spot -symbol BTCUSDT -trades=true'

# 启动 Subscriber
Start-Process powershell -ArgumentList '-NoExit', '-Command', 'go run ./cmd/subscriber -topic orderbook.band.BTCUSDT -trades-topic trades.BTCUSDT'
```