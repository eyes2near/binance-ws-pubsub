package binance

// 说明：目前 trades worker 直接把 Binance 返回的 trade JSON 当作 RawMessage 转发，
// 这个文件主要用于将来你想“解析后再发布/过滤/采样”时有类型可用。
// spot / coin-m 的 trade stream 字段非常接近，因此用同一结构体即可（字段按官方文档）。
type TradeEvent struct {
	EventType string `json:"e"` // "trade"
	EventTime int64  `json:"E"`
	Symbol    string `json:"s"`

	TradeID int64  `json:"t"`
	Price   string `json:"p"`
	Qty     string `json:"q"`

	BuyerOrderID  int64 `json:"b"`
	SellerOrderID int64 `json:"a"`

	TradeTime    int64 `json:"T"`
	IsBuyerMaker bool  `json:"m"`

	Ignore bool `json:"M"` // spot has "M": ignore; may be absent in some markets
}
