package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// ==================== 数据结构 ====================

// ExchangeInfo 交易所信息响应
type ExchangeInfo struct {
	Symbols []SymbolInfo `json:"symbols"`
}

// SymbolInfo 交易对信息
type SymbolInfo struct {
	Symbol         string `json:"symbol"`
	Pair           string `json:"pair"`
	ContractType   string `json:"contractType"`
	DeliveryDate   int64  `json:"deliveryDate"`
	ContractStatus string `json:"contractStatus"`
}

// DepthUpdate 深度更新消息
type DepthUpdate struct {
	EventType     string     `json:"e"`
	EventTime     int64      `json:"E"`
	TransactTime  int64      `json:"T"`
	Symbol        string     `json:"s"`
	Pair          string     `json:"ps"`
	FirstUpdateID int64      `json:"U"`
	FinalUpdateID int64      `json:"u"`
	PrevUpdateID  int64      `json:"pu"`
	Bids          [][]string `json:"b"`
	Asks          [][]string `json:"a"`
}

// PartialDepth 部分深度快照
type PartialDepth struct {
	LastUpdateID int64      `json:"lastUpdateId"`
	EventTime    int64      `json:"E"`
	TransactTime int64      `json:"T"`
	Bids         [][]string `json:"bids"`
	Asks         [][]string `json:"asks"`
}

// StreamMessage WebSocket流消息包装
type StreamMessage struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

// OrderBook 订单簿
type OrderBook struct {
	Symbol       string
	LastUpdateID int64
	UpdateTime   time.Time
	Bids         []PriceLevel
	Asks         []PriceLevel
	mu           sync.RWMutex
}

// PriceLevel 价格档位
type PriceLevel struct {
	Price    string
	Quantity string
}

// ==================== 常量 ====================

const (
	// Binance COIN-M API endpoints
	RestBaseURL = "https://dapi.binance.com"
	WsBaseURL   = "wss://dstream.binance.com"

	// 深度档位
	DepthLevels = 20

	// 更新频率
	UpdateSpeed = "100ms"
)

// ==================== 主程序 ====================

func main() {
	fmt.Println("========================================")
	fmt.Println("  Binance COIN-M BTCUSD 订单簿监控")
	fmt.Println("========================================")

	// 1. 获取当季和次季合约符号
	contracts, err := getQuarterlyContracts()
	if err != nil {
		log.Fatalf("❌ 获取合约信息失败: %v", err)
	}

	if len(contracts) == 0 {
		log.Fatal("❌ 未找到任何季度合约")
	}

	fmt.Println("\n📋 找到的季度合约:")
	for _, c := range contracts {
		deliveryTime := time.UnixMilli(c.DeliveryDate)
		fmt.Printf("   - %s (%s) 交割日期: %s\n",
			c.Symbol, c.ContractType, deliveryTime.Format("2006-01-02"))
	}

	// 2. 创建订单簿存储
	orderBooks := make(map[string]*OrderBook)
	for _, c := range contracts {
		orderBooks[c.Symbol] = &OrderBook{Symbol: c.Symbol}
	}

	// 3. 构建WebSocket URL
	var streams []string
	for _, c := range contracts {
		// 使用部分深度流
		stream := fmt.Sprintf("%s@depth%d@%s",
			strings.ToLower(c.Symbol), DepthLevels, UpdateSpeed)
		streams = append(streams, stream)
	}

	wsURL := fmt.Sprintf("%s/stream?streams=%s", WsBaseURL, strings.Join(streams, "/"))
	fmt.Printf("\n🔗 WebSocket URL: %s\n", wsURL)

	// 4. 启动WebSocket连接
	ctx := &AppContext{
		orderBooks: orderBooks,
		done:       make(chan struct{}),
	}

	// 处理优雅退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n\n⚠️  收到退出信号，正在关闭...")
		close(ctx.done)
	}()

	// 启动WebSocket
	if err := runWebSocket(ctx, wsURL); err != nil {
		log.Fatalf("❌ WebSocket错误: %v", err)
	}
}

// ==================== 应用上下文 ====================

type AppContext struct {
	orderBooks map[string]*OrderBook
	conn       *websocket.Conn
	done       chan struct{}
	mu         sync.Mutex
}

// ==================== REST API ====================

func getQuarterlyContracts() ([]SymbolInfo, error) {
	url := RestBaseURL + "/dapi/v1/exchangeInfo"
	fmt.Printf("\n📡 正在获取交易对信息: %s\n", url)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP状态码: %d", resp.StatusCode)
	}

	var exchangeInfo ExchangeInfo
	if err := json.NewDecoder(resp.Body).Decode(&exchangeInfo); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %w", err)
	}

	// 过滤BTCUSD的当季和次季合约
	var quarterlyContracts []SymbolInfo
	for _, s := range exchangeInfo.Symbols {
		if s.Pair == "BTCUSD" &&
			(s.ContractType == "CURRENT_QUARTER" || s.ContractType == "NEXT_QUARTER") &&
			s.ContractStatus == "TRADING" {
			quarterlyContracts = append(quarterlyContracts, s)
		}
	}

	// 按交割日期排序
	sort.Slice(quarterlyContracts, func(i, j int) bool {
		return quarterlyContracts[i].DeliveryDate < quarterlyContracts[j].DeliveryDate
	})

	return quarterlyContracts, nil
}

// ==================== WebSocket ====================

func runWebSocket(ctx *AppContext, wsURL string) error {
	fmt.Println("\n🚀 正在连接WebSocket...")

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket连接失败: %w", err)
	}
	defer conn.Close()

	ctx.mu.Lock()
	ctx.conn = conn
	ctx.mu.Unlock()

	fmt.Println("✅ WebSocket连接成功!")
	fmt.Println("\n📊 开始接收订单簿数据 (按 Ctrl+C 退出).")
	fmt.Println(strings.Repeat("=", 80))

	// 设置ping/pong处理
	conn.SetPongHandler(func(appData string) error {
		return nil
	})

	// 启动心跳
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx.mu.Lock()
				if ctx.conn != nil {
					ctx.conn.WriteMessage(websocket.PingMessage, nil)
				}
				ctx.mu.Unlock()
			case <-ctx.done:
				return
			}
		}
	}()

	// 读取消息循环
	for {
		select {
		case <-ctx.done:
			return nil
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				select {
				case <-ctx.done:
					return nil
				default:
					return fmt.Errorf("读取消息失败: %w", err)
				}
			}

			handleMessage(ctx, message)
		}
	}
}

func handleMessage(ctx *AppContext, message []byte) {
	var streamMsg StreamMessage
	if err := json.Unmarshal(message, &streamMsg); err != nil {
		log.Printf("⚠️  解析流消息失败: %v", err)
		return
	}

	// 从stream名称中提取symbol
	parts := strings.Split(streamMsg.Stream, "@")
	if len(parts) < 1 {
		return
	}
	symbol := strings.ToUpper(parts[0])

	// 解析深度数据
	var depth PartialDepth
	if err := json.Unmarshal(streamMsg.Data, &depth); err != nil {
		// 尝试解析为DepthUpdate格式
		var depthUpdate DepthUpdate
		if err2 := json.Unmarshal(streamMsg.Data, &depthUpdate); err2 != nil {
			log.Printf("⚠️  解析深度数据失败: %v", err)
			return
		}
		// 转换格式
		depth = PartialDepth{
			LastUpdateID: depthUpdate.FinalUpdateID,
			EventTime:    depthUpdate.EventTime,
			TransactTime: depthUpdate.TransactTime,
			Bids:         depthUpdate.Bids,
			Asks:         depthUpdate.Asks,
		}
	}

	// 更新订单簿
	updateOrderBook(ctx, symbol, &depth)

	// 打印订单簿
	printOrderBook(ctx, symbol)
}

func updateOrderBook(ctx *AppContext, symbol string, depth *PartialDepth) {
	ob, exists := ctx.orderBooks[symbol]
	if !exists {
		return
	}

	ob.mu.Lock()
	defer ob.mu.Unlock()

	ob.LastUpdateID = depth.LastUpdateID
	if depth.EventTime > 0 {
		ob.UpdateTime = time.UnixMilli(depth.EventTime)
	} else {
		ob.UpdateTime = time.Now()
	}

	// 更新买单
	ob.Bids = make([]PriceLevel, len(depth.Bids))
	for i, bid := range depth.Bids {
		if len(bid) >= 2 {
			ob.Bids[i] = PriceLevel{Price: bid[0], Quantity: bid[1]}
		}
	}

	// 更新卖单
	ob.Asks = make([]PriceLevel, len(depth.Asks))
	for i, ask := range depth.Asks {
		if len(ask) >= 2 {
			ob.Asks[i] = PriceLevel{Price: ask[0], Quantity: ask[1]}
		}
	}
}

func printOrderBook(ctx *AppContext, symbol string) {
	ob, exists := ctx.orderBooks[symbol]
	if !exists {
		return
	}

	ob.mu.RLock()
	defer ob.mu.RUnlock()

	// 清屏效果 - 使用分隔线代替
	fmt.Printf("\n📈 【%s】订单簿 @ %s\n",
		symbol, ob.UpdateTime.Format("15:04:05.000"))
	fmt.Printf("   LastUpdateID: %d\n", ob.LastUpdateID)
	fmt.Println(strings.Repeat("-", 50))

	// 打印卖单 (从高到低，显示前5档)
	fmt.Println("   🔴 卖单 (Asks):")
	askCount := min(5, len(ob.Asks))
	for i := askCount - 1; i >= 0; i-- {
		fmt.Printf("      [%d] 价格: %12s | 数量: %10s 张\n",
			i+1, ob.Asks[i].Price, ob.Asks[i].Quantity)
	}

	fmt.Println("   " + strings.Repeat("─", 44))

	// 打印买单 (从高到低，显示前5档)
	fmt.Println("   🟢 买单 (Bids):")
	bidCount := min(5, len(ob.Bids))
	for i := 0; i < bidCount; i++ {
		fmt.Printf("      [%d] 价格: %12s | 数量: %10s 张\n",
			i+1, ob.Bids[i].Price, ob.Bids[i].Quantity)
	}

	// 计算并显示买卖价差
	if len(ob.Bids) > 0 && len(ob.Asks) > 0 {
		fmt.Printf("\n   📊 价差: %s - %s\n", ob.Asks[0].Price, ob.Bids[0].Price)
	}

	fmt.Println(strings.Repeat("=", 50))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
