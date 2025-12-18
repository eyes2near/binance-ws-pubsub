项目结构图如下:
binance-ws-pubsub/
├───cmd/
│   ├───broker/
│   │   └───main.go
│   ├───publisher/
│   │   └───main.go
│   └───subscriber/
│       ├───main.go
│       ├───ui.go
│       ├───vt_other.go
│       └───vt_windows.go
├───internal/
│   ├───binance/
│   │   └───depth.go
│   ├───broker/
│   │   ├───broker.go
│   │   └───protocol.go
│   └───orderbook/
│       └───book.go
├───README.md
├───go.mod
└───go.sum
