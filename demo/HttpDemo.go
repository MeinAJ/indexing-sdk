package demo

import (
	"fmt"
	"github.com/MeinAJ/indexing-sdk/client"
	"time"
)

func TestHttp() {

	config := &client.Config{
		BaseURL:        "http://127.0.0.1:8080",
		RequestTimeout: time.Duration(10) * time.Second,
	}

	// 创建客户端
	eventsClient := client.NewEventsClient(config)

	// 查询事件
	req := &client.FlowEventsRequest{
		FromBlock:  9760967,
		Address:    "0x912521E54FE0a060652d467D42efEa3AF007a4e3",
		EventNames: []string{"Transfer"},
	}

	var dataChannel = make(chan client.EventData)
	var committedChannel = make(chan interface{})

	err := eventsClient.SubscribeEvents(req, dataChannel, committedChannel)
	if err != nil {
		fmt.Println(err)
		return
	}
}
