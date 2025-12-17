package demo

import (
	"encoding/json"
	"fmt"
	"indexing-sdk/client"
	"log"
)

func TestHttp() {
	// 创建客户端
	eventsClient := client.NewEventsClient(client.Config{
		BaseURL: "http://127.0.0.1:8080",
	})

	// 查询事件
	req := client.GetEventsRequest{
		FromBlock:  9760967,
		ToBlock:    9761177,
		PageNumber: 1,
		PageSize:   1,
	}

	resp, err := eventsClient.GetEvents(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	// 打印resp，转成json字符串
	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		log.Printf("JSON序列化失败: %v", err)
		return
	}
	fmt.Printf("JSON字符串: %s\n", string(jsonBytes))
}
