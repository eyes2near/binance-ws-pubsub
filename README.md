# binance-ws-pubsub

快速入门：如何运行 `broker`、`publisher` 和 `subscriber` (Windows PowerShell)

## 先决条件
- 已安装 Go 工具链 (推荐 go 1.20+)
- 可以访问币安网络 (publisher 需要)

## 推荐顺序
1. 启动 broker (WebSocket 服务器)
2. 启动 publisher (从币安获取订单簿快照)
3. 启动 subscriber (订阅主题并显示快照)

## 运行组件

在单独的 PowerShell 终端中按顺序运行每个组件。

### 1. 启动 Broker

Broker 是 WebSocket 服务器，负责在发布者和订阅者之间中继消息。

```powershell
# 默认监听 :8080
go run ./cmd/broker
```
- 使用 `-listen` 标志更改监听地址 (例如, `-listen :9000`)。

### 2. 启动 Publisher

Publisher 从币安获取订单簿数据并将其发布到 Broker。

Publisher 支持两种市场模式: `spot` (现货) 和 `coin-m` (币本位合约)。

#### 现货市场 (默认模式)

- `-market=spot` 是默认值, 可以省略。
- `-symbol` 应为完整的交易对 (例如 `BTCUSDT`)。

```powershell
# 订阅 BTCUSDT 现货市场的订单簿
go run ./cmd/publisher -symbol BTCUSDT
```

#### COIN-M 市场 (币本位合约)

- 使用 `-market coin-m` 来激活。
- `-symbol` 应为基础币种 (例如 `BTC`, `ETH`)。
- `-contract` 用于指定合约类型: `current` (当季) 或 `next` (次季)。

```powershell
# 订阅 BTC/USD 当季合约的订单簿
go run ./cmd/publisher -market coin-m -symbol BTC -contract current

# 订阅 ETH/USD 次季合约的订单簿
go run ./cmd/publisher -market coin-m -symbol ETH -contract next
```

### 3. 启动 Subscriber

Subscriber 连接到 Broker，订阅一个主题并实时显示订单簿快照。

- `-topic` 必须与 `publisher` 生成的主题匹配。主题格式为 `orderbook.band.{SYMBOL}`。

```powershell
# 订阅 BTCUSDT 现货主题
go run ./cmd/subscriber -topic orderbook.band.BTCUSDT

# 订阅 BTC/USD 当季合约主题 (注意合约符号会自动解析)
# 你需要先运行publisher来查看确切的符号, 例如 BTCUSD_231229
go run ./cmd/subscriber -topic orderbook.band.BTCUSD_231229
```

## 注意事项
- `publisher` 和 `subscriber` 默认连接到 Broker 的 `ws://localhost:8080/ws`。使用 `-broker` 标志来更改地址。
- 在终端中按 `Ctrl+C` 可以平滑地停止任何进程。

## 后台运行 (可选)
- 最简单的方法是打开单独的终端并运行上面的命令。
- 对于 Windows 上的脚本/自动化，请考虑使用 `Start-Process` 在单独的窗口中生成进程。

### `Start-Process` 示例 (PowerShell)

```powershell
cd C:\Users\wangn\go-projects\binance-ws-pubsub
Start-Process powershell -ArgumentList '-NoExit', '-Command', 'go run ./cmd/broker'
Start-Process powershell -ArgumentList '-NoExit', '-Command', 'go run ./cmd/publisher -symbol BTCUSDT'
Start-Process powershell -ArgumentList '-NoExit', '-Command', 'go run ./cmd/subscriber -topic orderbook.band.BTCUSDT'
```