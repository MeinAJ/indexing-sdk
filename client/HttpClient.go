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
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
	debug      bool
}

// Config 客户端配置
type Config struct {
	BaseURL string        // 基础URL，如 "http://127.0.0.1:8080"
	Timeout time.Duration // 超时时间
	Debug   bool          // 调试模式
}

// NewEventsClient 创建新的客户端实例
func NewEventsClient(config Config) *EventsClient {
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

// GetEventsRequest HTTP请求参数
type GetEventsRequest struct {
	FromBlock  int    `json:"fromBlock"`
	ToBlock    int    `json:"toBlock"`
	Address    string `json:"address,omitempty"`
	PageNumber int    `json:"pageNumber"`
	PageSize   int    `json:"pageSize"`
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
	EventName        string    `json:"eventName"`
	EventSignature   string    `json:"eventSignature"`
	Topics           []string  `json:"topics"`
	Data             string    `json:"data"`
	Removed          bool      `json:"removed"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// GetEventsResponse HTTP响应
type GetEventsResponse struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Data    []Event  `json:"data"`
	Meta    MetaData `json:"meta,omitempty"`
}

// MetaData 分页元数据
type MetaData struct {
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	Total      int `json:"total"`
	TotalPages int `json:"totalPages"`
}

// GetEvents HTTP请求方法
func (c *EventsClient) GetEvents(req GetEventsRequest) (*GetEventsResponse, error) {
	url := fmt.Sprintf("%s/api/v1/event/list", c.baseURL)

	// 序列化请求体
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}

	var lastErr error

	// 最多重试3次
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
			lastErr = fmt.Errorf("create request failed: %w", err)
			continue // 继续重试
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")

		// 发送请求
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("http request failed (attempt %d): %w", attempt+1, err)
			continue // 继续重试
		}

		// 读取响应
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response failed (attempt %d): %w", attempt+1, err)
			continue // 继续重试
		}

		// 解析响应
		var apiResult GetEventsResponse
		if err := json.Unmarshal(body, &apiResult); err != nil {
			lastErr = fmt.Errorf("unmarshal response failed (attempt %d): %w", attempt+1, err)
			continue // 继续重试
		}

		// 检查API业务错误码
		if apiResult.Code != 200 && apiResult.Code != 0 {
			// 如果是业务逻辑错误，通常不需要重试（除非某些特定错误码需要重试）
			// 这里可以根据具体业务需求调整，比如某些特定错误码也需要重试
			lastErr = fmt.Errorf("api error (attempt %d): %s", attempt+1, apiResult.Message)
			// 如果是网络错误或服务器错误，继续重试；如果是业务错误，直接返回
			if !shouldRetry(apiResult.Code) {
				return nil, lastErr
			}
			continue
		}

		// 成功，返回结果
		return &apiResult, nil
	}

	// 所有重试都失败
	return nil, fmt.Errorf("all 3 attempts failed, last error: %w", lastErr)
}

// shouldRetry 根据错误码判断是否需要重试
// 可以根据实际业务需求调整这个逻辑
func shouldRetry(code int) bool {
	// 通常5xx错误表示服务器错误，应该重试
	// 4xx错误通常表示客户端错误，不应该重试（除非某些特殊情况）
	if code >= 500 && code < 600 {
		return true
	}

	// 特定业务错误码，根据实际情况调整
	// 例如：限流、服务暂时不可用等错误可以重试
	retryableCodes := map[int]bool{
		429: true, // Too Many Requests
		503: true, // Service Unavailable
		504: true, // Gateway Timeout
	}

	return retryableCodes[code]
}
