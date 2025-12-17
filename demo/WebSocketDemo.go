package demo

import (
	"encoding/json"
	"fmt"
	"github.com/MeinAJ/indexing-sdk/client"
)

func TestWebsocket() {
	config := client.StreamConfig{
		URL: "ws://127.0.0.1:8080/api/v1/event/ws/stream",
		Request: client.WSRequest{
			FromBlock: 9760967,
			ToBlock:   9761177,
		},
		OnEvents: func(events []client.Event, page, total int) {
			// 处理事件
			fmt.Println(json.Marshal(events))
		}}
	// 创建流式客户端
	streamClient := client.NewStreamClient(config)
	// 启动
	err := streamClient.Start(config)
	if err != nil {
		_ = fmt.Errorf("streamClient start failed: %w", err)
	}
}
