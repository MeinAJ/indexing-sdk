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
	baseURL     string
	httpClient  *http.Client
	timeout     time.Duration
	period      time.Duration
	debug       bool
	dataChannel chan []*Event
}

// Config 客户端配置
type Config struct {
	BaseURL string        // 基础URL，如 "http://127.0.0.1:8080"
	Timeout time.Duration // 超时时间
	Debug   bool          // 调试模式
}

// NewEventsClient 创建新的客户端实例
func NewEventsClient(config *Config) *EventsClient {
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}

	return &EventsClient{
		baseURL: config.BaseURL,
		httpClient: &http.Client{
			Timeout: config.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		timeout: config.Timeout,
		debug:   config.Debug,
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

type Page struct {
	Page              int      `json:"page"`                // 页码
	Size              int      `json:"size"`                // 每页大小
	Total             int      `json:"total"`               // 总数
	Data              []*Event `json:"data"`                // 数据
	LatestBlockNumber int64    `json:"latest_block_number"` // 最新区块号
}

// SubscribeEvents 模拟订阅事件
func (c *EventsClient) SubscribeEvents(req *FlowEventsRequest, dataChannel chan *EventData) error {
	timer := time.NewTimer(c.period)
	innerReq := &HttpEventsRequest{
		FromBlock:  req.FromBlock,
		ToBlock:    req.FromBlock + 10,
		Address:    req.Address,
		EventNames: req.EventNames,
		PageNumber: 1,
		PageSize:   100,
	}
	for {
		select {
		case <-timer.C:
			// 重新构造请求参数
			response, lastErr := c.GetEvents(innerReq)
			if lastErr != nil {
				timer.Reset(c.period)
				continue
			}
			metaData := &MetaData{
				ScanLatestBlockNumber: req.FromBlock + 10,
			}
			eventData := &EventData{
				Events:   response.Data.Data,
				MetaData: metaData,
			}
			// 重置请求参数
			if response.Data == nil || len(response.Data.Data) < 100 {
				// 1、没有数据或者条数不满足100条，表示这个区块范围，已经查询完了；重置区块号
				metaData.ScanLatestBlockCompleted = true
				innerReq.Reset(innerReq.ToBlock+1, innerReq.ToBlock+11, 1, 100)
			} else {
				// 2、区块范围还有数据时，页数+1
				metaData.ScanLatestBlockCompleted = false
				innerReq.Reset(innerReq.FromBlock, innerReq.ToBlock, innerReq.PageNumber+1, 100)
			}
			// 发送数据
			dataChannel <- eventData
			timer.Reset(c.period)
		}
	}
}

func (c *EventsClient) GetEvents(req *HttpEventsRequest) (*PageResponse, error) {
	url := fmt.Sprintf("%s/api/v1/event/list", c.baseURL)
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
		resp, err := c.httpClient.Do(httpReq)
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
		504: true, // Gateway Timeout
	}
	return retryableCodes[code]
}
