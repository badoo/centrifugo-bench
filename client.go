package main

// Connect, subscribe on channel, publish into channel, read presence and history info.

import (
	"flag"
	"fmt"
	"log"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/centrifugal/centrifuge-go"
	"github.com/centrifugal/centrifugo/libcentrifugo/auth"
	"strconv"
)

type config struct {
	secret       string
	wsUrl        string
	channelName  string
	clientsCount uint

}

var Config config

var msgReceived int32 = 0
var clientsConnected int32 = 0

func parseFlags () {
	flag.StringVar(&Config.secret, "secret", "", "Secret.")
	flag.StringVar(&Config.wsUrl, "url", "", "WS URL, e.g.: ws://localhost:8000/connection/websocket.")
	flag.StringVar(&Config.channelName, "channel", "test", "Channel name.")
	flag.UintVar(&Config.clientsCount, "clients", 1, "Clients count.")

	flag.Parse()
}

func credentials(user int) *centrifuge.Credentials {
	// User ID
	user_str := strconv.Itoa(user)

	// Current timestamp as string.
	timestamp := centrifuge.Timestamp()

	// Empty info.
	info := ""

	// Generate client token so Centrifugo server can trust connection parameters received from client.
	token := auth.GenerateClientToken(Config.secret, user_str, timestamp, info)

	return &centrifuge.Credentials{
		User:      user_str,
		Timestamp: timestamp,
		Info:      info,
		Token:     token,
	}
}

func newConnection(user int) {
	creds := credentials(user)

	events := &centrifuge.EventHandler{
		OnDisconnect: func(c centrifuge.Centrifuge) error {
			log.Println("Disconnected")
			err := c.Reconnect(centrifuge.DefaultBackoffReconnect)
			if err != nil {
				log.Println(fmt.Sprintf("Failed to reconnect: %s", err.Error()))
				atomic.AddInt32(&clientsConnected, -1)
			} else {
				log.Println("Reconnected")
			}
			return nil
		},
	}

	conf := centrifuge.DefaultConfig
	conf.Timeout = 10 * time.Second

	c := centrifuge.NewCentrifuge(Config.wsUrl, creds, events, conf)

	err := c.Connect()
	if err != nil {
		log.Fatalln(fmt.Sprintf("Failed to connect: %s", err.Error()))
	}

	atomic.AddInt32(&clientsConnected, 1)

	onMessage := func(sub centrifuge.Sub, msg centrifuge.Message) error {
		atomic.AddInt32(&msgReceived, 1)
		return nil
	}

	subEvents := &centrifuge.SubEventHandler{
		OnMessage: onMessage,
	}

	sub, err := c.Subscribe(Config.channelName, subEvents)
	if err != nil {
		log.Fatalln(fmt.Sprintf("Failed to subscribe to channel %s: %s", Config.channelName, err.Error()))
	}

	msgs, err := sub.History()
	if err != nil {
		log.Fatalln(fmt.Sprintf("Error retreiving channel history: %s", err.Error()))
	} else {
		log.Printf("%d messages in channel history", len(msgs))
	}
}


func main() {
	started := time.Now()

	parseFlags()

	runtime.GOMAXPROCS(runtime.NumCPU()) // just to be sure :)

	for i := 0; i < int(Config.clientsCount); i++ {
		time.Sleep(time.Millisecond * 10)
		newConnection(i)
	}

	var prevMsgReceived int32 = 0

	for {
		time.Sleep(time.Second)
		currMsgReceived := atomic.LoadInt32(&msgReceived)
		currClientsConnected := atomic.LoadInt32(&clientsConnected)
		log.Printf(
			"Messages received: %d total,\t%d per second,\t%d per client per second,\t%d clients connected",
			currMsgReceived,
			currMsgReceived - prevMsgReceived,
			int(float32(currMsgReceived - prevMsgReceived) / float32(Config.clientsCount)),
			currClientsConnected)
		prevMsgReceived = currMsgReceived
	}

	log.Printf("%s", time.Since(started))
}
