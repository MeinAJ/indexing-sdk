package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// EventsClient 事件查询客户端
type EventsClient struct {
	BaseURL        string
	HttpClient     *http.Client
	RequestTimeout time.Duration
	RequestPeriod  time.Duration
	Debug          bool
	EventSize      int
	BlockSize      int
}

// Config 客户端配置
type Config struct {
	BaseURL        string        // 基础URL，如 "http://127.0.0.1:8080"
	RequestTimeout time.Duration // 超时时间
	RequestPeriod  time.Duration // 轮询间隔
	Debug          bool          // 调试模式
	EventSize      int           // 批量获取事件数量
	BlockSize      int           // 批量获取事件块大小
}

// NewEventsClient 创建新的客户端实例
func NewEventsClient(config *Config) *EventsClient {
	if config.RequestTimeout == 0 {
		config.RequestTimeout = 5 * time.Second
	}
	if config.RequestPeriod == 0 {
		config.RequestPeriod = 5 * time.Second
	}
	if config.BlockSize == 0 {
		config.BlockSize = 10
	}
	if config.EventSize == 0 {
		config.EventSize = 100
	}
	return &EventsClient{
		BaseURL: config.BaseURL,
		HttpClient: &http.Client{
			Timeout: config.RequestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		RequestPeriod:  config.RequestPeriod,
		RequestTimeout: config.RequestTimeout,
		Debug:          config.Debug,
		EventSize:      config.EventSize,
		BlockSize:      config.BlockSize,
	}
}

// FlowEventsRequest HTTP请求参数
type FlowEventsRequest struct {
	FromBlock  int      `json:"fromBlock"`
	Address    string   `json:"address,omitempty"`
	EventNames []string `json:"eventNames,omitempty"`
}

type HttpEventsRequest struct {
	FromBlock  int      `json:"fromBlock"`
	ToBlock    int      `json:"toBlock"`
	EventNames []string `json:"eventNames"`
	Address    string   `json:"address,omitempty"`
	PageNumber int      `json:"pageNumber"`
	PageSize   int      `json:"pageSize"`
}

func (request *HttpEventsRequest) Reset(fromBlock int, toBlock int, pageNumber int, pageSize int) {
	request.FromBlock = fromBlock
	request.ToBlock = toBlock
	request.PageNumber = pageNumber
	request.PageSize = pageSize
}

type MetaData struct {
	ScanLatestBlockNumber    int  `json:"scanLatestBlockNumber"`    // 最后扫描区块号
	ScanLatestBlockCompleted bool `json:"ScanLatestBlockCompleted"` // 最后扫描区块是否扫描完成
}

type EventData struct {
	MetaData *MetaData `json:"metaData"`
	Events   []*Event  `json:"events"`
}

// Event 事件数据结构
type Event struct {
	ID               uint64    `json:"id"`
	BlockNumber      uint64    `json:"blockNumber"`
	BlockHash        string    `json:"blockHash"`
	TransactionHash  string    `json:"transactionHash"`
	TransactionIndex uint      `json:"transactionIndex"`
	LogIndex         uint      `json:"logIndex"`
	ContractAddress  string    `json:"contractAddress"`
	UserAddress      string    `json:"userAddress"`
	TradeFee         uint64    `json:"tradeFee"`
	TradeFeeCurrency string    `json:"tradeFeeCurrency"`
	EventUniqueHash  string    `json:"eventUniqueHash"`
	EventName        string    `json:"eventName"`
	EventSignature   string    `json:"eventSignature"`
	Topics           []string  `json:"topics"`
	Data             string    `json:"data"`
	Removed          bool      `json:"removed"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type PageResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    *Page  `json:"data"`
}

type Response struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    *LatestBlockNumber `json:"data"`
}

type LatestBlockNumber struct {
	LatestBlockNumber uint64 `json:"latestBlockNumber"` // 最新区块号
}

type Page struct {
	Page  int      `json:"page"`  // 页码
	Size  int      `json:"size"`  // 每页大小
	Total int      `json:"total"` // 总数
	Data  []*Event `json:"data"`  // 数据
}

// SubscribeEvents 模拟订阅事件
func (c *EventsClient) SubscribeEvents(req *FlowEventsRequest, dataChannel chan *EventData, committedChannel chan interface{}) error {
	innerReq := &HttpEventsRequest{
		FromBlock:  req.FromBlock,
		ToBlock:    req.FromBlock + c.BlockSize,
		Address:    req.Address,
		EventNames: req.EventNames,
		PageNumber: 1,
		PageSize:   c.EventSize,
	}
	go func(innerReq *HttpEventsRequest, dataChannel chan *EventData, committedChannel chan interface{}) {
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				c.CycleGetEvents(innerReq, dataChannel, committedChannel, timer)
			}
		}
	}(innerReq, dataChannel, committedChannel)
	return nil
}

func (c *EventsClient) CycleGetEvents(innerReq *HttpEventsRequest, dataChannel chan *EventData, committedChannel chan interface{}, timer *time.Timer) {
	defer timer.Reset(c.RequestPeriod)
	latestBlockNumber, err := c.GetLatestBlockNumber()
	if err != nil {
		fmt.Printf("get latest block number failed: %s\n", err)
		return
	}
	if innerReq.ToBlock > int(latestBlockNumber) {
		fmt.Printf("toBlock(%d) > latestBlockNumber(%d), wait for next cycle", innerReq.ToBlock, latestBlockNumber)
		return
	}
	// 重新构造请求参数
	response, err := c.GetEvents(innerReq)
	if err != nil {
		fmt.Printf("get events failed: %s\n", err)
		return
	}
	metaData := &MetaData{
		ScanLatestBlockNumber: innerReq.FromBlock + c.BlockSize,
	}
	eventData := &EventData{
		Events:   response.Data.Data,
		MetaData: metaData,
	}
	// 重置请求参数
	total := response.Data.Total
	size := response.Data.Size
	page := response.Data.Page
	if response.Data == nil || len(response.Data.Data) < c.EventSize || (page*size == total) {
		// 1、没有数据或者条数不满足100条，表示这个区块范围，已经查询完了；重置区块号
		metaData.ScanLatestBlockCompleted = true
		innerReq.Reset(innerReq.ToBlock+1, innerReq.ToBlock+11, 1, c.EventSize)
	} else {
		// 2、区块范围还有数据时，页数+1
		metaData.ScanLatestBlockCompleted = false
		innerReq.Reset(innerReq.FromBlock, innerReq.ToBlock, innerReq.PageNumber+1, c.EventSize)
	}
	// 发送数据
	dataChannel <- eventData
	// 如果设置了commit channel，会等待消费者消费完成，才查询后续的数据
	if committedChannel != nil {
		<-committedChannel
	}
}

func (c *EventsClient) GetLatestBlockNumber() (uint64, error) {
	url := fmt.Sprintf("%s/api/v1/event/latestBlockNumber", c.BaseURL)
	var lastErr error = nil
	// 最多重试3次
	for attempt := 0; attempt < 3; attempt++ {
		// 如果不是第一次尝试，等待一段时间（指数退避）
		if attempt > 0 {
			// 指数退避：第一次重试等1秒，第二次等2秒
			waitTime := time.Duration(attempt) * time.Second
			time.Sleep(waitTime)
		}
		// 创建HTTP请求
		httpReq, err := http.NewRequest("GET", url, nil)
		if err != nil {
			lastErr = fmt.Errorf("create http request failed: %w", err)
			continue // 继续重试
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		// 发送请求
		resp, err := c.HttpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("http request failed: %w", err)
			continue // 继续重试
		}
		// 读取响应
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response body failed: %w", err)
			continue // 继续重试
		}
		// 解析响应
		var apiResult Response
		if err := json.Unmarshal(body, &apiResult); err != nil {
			lastErr = fmt.Errorf("unmarshal response body failed: %w", err)
			continue // 继续重试
		}
		// 检查API业务错误码
		if apiResult.Code != 200 && apiResult.Code != 0 {
			// 如果是业务逻辑错误，通常不需要重试（除非某些特定错误码需要重试）
			// 这里可以根据具体业务需求调整，比如某些特定错误码也需要重试
			lastErr = fmt.Errorf("error getting events: %s", apiResult.Message)
			// 如果是网络错误或服务器错误，继续重试；如果是业务错误，直接返回
			if !shouldRetry(apiResult.Code) {
				return 0, lastErr
			}
			continue
		}
		// 成功，返回结果
		return apiResult.Data.LatestBlockNumber, nil
	}
	return 0, lastErr
}

func (c *EventsClient) GetEvents(req *HttpEventsRequest) (*PageResponse, error) {
	url := fmt.Sprintf("%s/api/v1/event/list", c.BaseURL)
	var lastErr error = nil
	// 最多重试3次
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		// 如果不是第一次尝试，等待一段时间（指数退避）
		if attempt > 0 {
			// 指数退避：第一次重试等1秒，第二次等2秒
			waitTime := time.Duration(attempt) * time.Second
			time.Sleep(waitTime)
		}
		// 创建HTTP请求
		httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			lastErr = fmt.Errorf("create http request failed: %w", err)
			continue // 继续重试
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		// 发送请求
		resp, err := c.HttpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("http request failed: %w", err)
			continue // 继续重试
		}
		// 读取响应
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response body failed: %w", err)
			continue // 继续重试
		}
		// 解析响应
		var apiResult PageResponse
		if err := json.Unmarshal(body, &apiResult); err != nil {
			lastErr = fmt.Errorf("unmarshal response body failed: %w", err)
			continue // 继续重试
		}
		// 检查API业务错误码
		if apiResult.Code != 200 && apiResult.Code != 0 {
			// 如果是业务逻辑错误，通常不需要重试（除非某些特定错误码需要重试）
			// 这里可以根据具体业务需求调整，比如某些特定错误码也需要重试
			lastErr = fmt.Errorf("error getting events: %s", apiResult.Message)
			// 如果是网络错误或服务器错误，继续重试；如果是业务错误，直接返回
			if !shouldRetry(apiResult.Code) {
				return nil, lastErr
			}
			continue
		}
		// 成功，返回结果
		return &apiResult, nil
	}
	return nil, lastErr
}

// shouldRetry 根据错误码判断是否需要重试
func shouldRetry(code int) bool {
	// 通常5xx错误表示服务器错误，应该重试
	// 4xx错误通常表示客户端错误，不应该重试（除非某些特殊情况）
	if code >= 500 && code < 600 {
		return true
	}
	retryableCodes := map[int]bool{
		429: true, // Too Many Requests
		503: true, // Service Unavailable
		504: true, // Gateway RequestTimeout
	}
	return retryableCodes[code]
}
