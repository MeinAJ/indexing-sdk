package client

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSClient WebSocket客户端
type WSClient struct {
	conn        *websocket.Conn
	url         string
	onMessage   func(*WSMessage)
	onError     func(error)
	onClose     func()
	pingPeriod  time.Duration
	done        chan struct{}
	mu          sync.RWMutex
	isConnected bool
}

// WSConfig WebSocket配置
type WSConfig struct {
	URL        string           // WebSocket URL
	OnMessage  func(*WSMessage) // 消息回调
	OnError    func(error)      // 错误回调
	OnClose    func()           // 关闭回调
	PingPeriod time.Duration    // Ping间隔
	BufferSize int              // 缓冲区大小
}

// WSMessage WebSocket消息结构
type WSMessage struct {
	Type    string          `json:"type"`    // "events", "error", "info", "heartbeat", "new_event"
	Message string          `json:"message"` // 消息内容
	Data    json.RawMessage `json:"data"`    // 数据（原始JSON）
	Page    int             `json:"page"`    // 当前页码
	Total   int             `json:"total"`   // 总数据量
}

// WSRequest WebSocket请求
type WSRequest struct {
	FromBlock int    `json:"fromBlock,omitempty"`
	ToBlock   int    `json:"toBlock,omitempty"`
	Address   string `json:"address,omitempty"`
}

// NewWSClient 创建WebSocket客户端
func NewWSClient(config WSConfig) *WSClient {
	if config.PingPeriod == 0 {
		config.PingPeriod = 30 * time.Second
	}

	return &WSClient{
		url:        config.URL,
		onMessage:  config.OnMessage,
		onError:    config.OnError,
		onClose:    config.OnClose,
		pingPeriod: config.PingPeriod,
		done:       make(chan struct{}),
	}
}

// Connect 连接到WebSocket服务器
func (c *WSClient) Connect() error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 45 * time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
	}

	conn, _, err := dialer.Dial(c.url, nil)
	if err != nil {
		return fmt.Errorf("connect to websocket failed: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.isConnected = true
	c.mu.Unlock()

	// 启动读写协程
	go c.readPump()
	go c.writePump()

	return nil
}

// SendRequest 发送请求
func (c *WSClient) SendRequest(req WSRequest) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isConnected || c.conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request failed: %w", err)
	}

	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// Close 关闭连接
func (c *WSClient) Close() error {
	close(c.done)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.isConnected = false
		c.conn = nil
		return err
	}
	return nil
}

// IsConnected 检查是否连接
func (c *WSClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isConnected
}

// readPump 读取消息
func (c *WSClient) readPump() {
	defer func() {
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.isConnected = false
		c.conn = nil
		c.mu.Unlock()

		if c.onClose != nil {
			c.onClose()
		}
	}()

	for {
		select {
		case <-c.done:
			return
		default:
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			if conn == nil {
				return
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					if c.onError != nil {
						c.onError(err)
					}
				}
				return
			}

			// 解析消息
			var wsMsg WSMessage
			if err := json.Unmarshal(message, &wsMsg); err != nil {
				if c.onError != nil {
					c.onError(fmt.Errorf("parse message failed: %w", err))
				}
				continue
			}

			if c.onMessage != nil {
				c.onMessage(&wsMsg)
			}
		}
	}
}

// writePump 写入消息和保持心跳
func (c *WSClient) writePump() {
	ticker := time.NewTicker(c.pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			if conn == nil {
				return
			}

			// 发送Ping
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				if c.onError != nil {
					c.onError(err)
				}
				return
			}
		}
	}
}
