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
	secret            string
	wsUrl             string
	channels          uint
	clientsPerChannel uint
}

var Config config

var channels map[int]string

var msgReceived int64 = 0
var clientsConnected int64 = 0

func parseFlags () {
	flag.StringVar(&Config.secret, "secret", "", "Secret.")
	flag.StringVar(&Config.wsUrl, "url", "", "WS URL, e.g.: ws://localhost:8000/connection/websocket.")
	flag.UintVar(&Config.channels, "channels", 1, "Channels count.")
	flag.UintVar(&Config.clientsPerChannel, "clients-per-channel", 1, "Clients per channel count.")

	flag.Parse()
}

func generateChannelsNames() {
	if Config.channels == 1 {
		channels[0] = "bench"
	} else {
		for i := 0; i < int(Config.channels); i++ {
			channels[i] = fmt.Sprintf("bench%d", i)
		}
	}
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

func newConnection(channel int, client int) {
	creds := credentials(channel * int(Config.clientsPerChannel) + client)

	events := &centrifuge.EventHandler{
		OnDisconnect: func(c centrifuge.Centrifuge) error {
			log.Println("Disconnected")
			err := c.Reconnect(centrifuge.DefaultBackoffReconnect)
			if err != nil {
				log.Println(fmt.Sprintf("Failed to reconnect: %s", err.Error()))
				atomic.AddInt64(&clientsConnected, -1)
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

	atomic.AddInt64(&clientsConnected, 1)

	onMessage := func(sub centrifuge.Sub, msg centrifuge.Message) error {
		atomic.AddInt64(&msgReceived, 1)
		return nil
	}

	subEvents := &centrifuge.SubEventHandler{
		OnMessage: onMessage,
	}

	sub, err := c.Subscribe(channels[channel], subEvents)
	if err != nil {
		log.Fatalln(fmt.Sprintf("Failed to subscribe to channel %s: %s", channels[channel], err.Error()))
	}

	msgs, err := sub.History()
	if err != nil {
		log.Fatalln(fmt.Sprintf("Error retreiving channel history: %s", err.Error()))
	} else {
		currClientsConnected := atomic.LoadInt64(&clientsConnected)
		log.Printf("%d messages in channel history, %d clients connected", len(msgs), currClientsConnected)
	}
}


func main() {
	started := time.Now()

	parseFlags()

	generateChannelsNames()

	runtime.GOMAXPROCS(runtime.NumCPU()) // just to be sure :)

	for i := 0; i < int(Config.channels); i++ {
		for j := 0; j < int(Config.clientsPerChannel); j++ {
			time.Sleep(time.Millisecond * 10)
			newConnection(i, j)
		}
	}

	var prevMsgReceived int64 = 0

	for {
		time.Sleep(time.Second)
		currMsgReceived := atomic.LoadInt64(&msgReceived)
		currClientsConnected := atomic.LoadInt64(&clientsConnected)
		log.Printf(
			"Messages received: %d total,\t%d per second,\t%d per client per second,\t%d clients connected",
			currMsgReceived,
			currMsgReceived - prevMsgReceived,
			int(float32(currMsgReceived - prevMsgReceived) / float32(Config.clientsPerChannel)),
			currClientsConnected)
		prevMsgReceived = currMsgReceived
	}

	log.Printf("%s", time.Since(started))
}
