// client/stream_client.go
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// StreamClient 流式客户端
type StreamClient struct {
	wsClient *WSClient
	buffer   chan *WSMessage
	ctx      context.Context
	cancel   context.CancelFunc
}

// StreamConfig 流式配置
type StreamConfig struct {
	URL           string
	Request       WSRequest
	BufferSize    int
	MaxRetries    int
	RetryInterval time.Duration
	OnEvents      func([]Event, int, int) // 事件回调：事件列表，页码，总数
	OnError       func(error)
}

// NewStreamClient 创建流式客户端
func NewStreamClient(config StreamConfig) *StreamClient {
	ctx, cancel := context.WithCancel(context.Background())

	return &StreamClient{
		buffer: make(chan *WSMessage, config.BufferSize),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start 开始流式监听
func (sc *StreamClient) Start(config StreamConfig) error {
	// 创建WebSocket客户端
	wsConfig := WSConfig{
		URL: config.URL,
		OnMessage: func(msg *WSMessage) {
			sc.buffer <- msg
		},
		OnError: config.OnError,
		OnClose: func() {
			log.Println("WebSocket connection closed")
		},
	}

	wsClient := NewWSClient(wsConfig)

	// 连接
	if err := wsClient.Connect(); err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}

	sc.wsClient = wsClient

	// 发送初始请求
	if err := wsClient.SendRequest(config.Request); err != nil {
		return fmt.Errorf("send request failed: %w", err)
	}

	// 处理消息
	go sc.processMessages(config)

	return nil
}

// processMessages 处理消息
func (sc *StreamClient) processMessages(config StreamConfig) {
	for {
		select {
		case <-sc.ctx.Done():
			return
		case msg := <-sc.buffer:
			switch msg.Type {
			case "events":
				var events []Event
				if err := json.Unmarshal(msg.Data, &events); err != nil {
					if config.OnError != nil {
						config.OnError(fmt.Errorf("parse events failed: %w", err))
					}
					continue
				}

				if config.OnEvents != nil {
					config.OnEvents(events, msg.Page, msg.Total)
				}

			case "new_event":
				var event Event
				if err := json.Unmarshal(msg.Data, &event); err != nil {
					if config.OnError != nil {
						config.OnError(fmt.Errorf("parse new event failed: %w", err))
					}
					continue
				}

				if config.OnEvents != nil {
					config.OnEvents([]Event{event}, 0, 0)
				}

			case "error":
				if config.OnError != nil {
					config.OnError(fmt.Errorf("server error: %s", msg.Message))
				}

			case "end":
				log.Printf("Stream ended: %s", msg.Message)
				return
			}
		}
	}
}

// Stop 停止流式监听
func (sc *StreamClient) Stop() {
	sc.cancel()
	if sc.wsClient != nil {
		_ = sc.wsClient.Close()
	}
	close(sc.buffer)
}
