package main

// Connect, subscribe on channel, publish into channel, read presence and history info.

import (
	"fmt"
	"log"
	"time"

	"github.com/centrifugal/centrifuge-go"
	"github.com/centrifugal/centrifugo/libcentrifugo/auth"
	"flag"
)

func main() {
	secretPtr := flag.String("secret", "", "Secret.")
	urlPtr := flag.String("url", "", "WS URL, e.g.: ws://localhost:8000/connection/websocket.")
	channelPtr := flag.String("channel", "test", "Channel name.")
	flag.Parse()

	// Application user ID.
	user := "42"

	// Current timestamp as string.
	timestamp := centrifuge.Timestamp()

	// Empty info.
	info := ""

	// Generate client token so Centrifugo server can trust connection parameters received from client.
	token := auth.GenerateClientToken(*secretPtr, user, timestamp, info)

	creds := &centrifuge.Credentials{
		User:      user,
		Timestamp: timestamp,
		Info:      info,
		Token:     token,
	}

	started := time.Now()

	conf := centrifuge.DefaultConfig
	conf.Timeout = 10 * time.Second
	c := centrifuge.NewCentrifuge(*urlPtr, creds, nil, conf)
	defer c.Close()

	err := c.Connect()
	if err != nil {
		log.Fatalln(err)
	}

	onMessage := func(sub centrifuge.Sub, msg centrifuge.Message) error {
		log.Println(fmt.Sprintf("New message received in channel %s: %#v", sub.Channel(), msg))
		return nil
	}

	onJoin := func(sub centrifuge.Sub, msg centrifuge.ClientInfo) error {
		log.Println(fmt.Sprintf("User %s joined channel %s with client ID %s", msg.User, sub.Channel(), msg.Client))
		return nil
	}

	onLeave := func(sub centrifuge.Sub, msg centrifuge.ClientInfo) error {
		log.Println(fmt.Sprintf("User %s with clientID left channel %s with client ID %s", msg.User, msg.Client, sub.Channel()))
		return nil
	}

	events := &centrifuge.SubEventHandler{
		OnMessage: onMessage,
		OnJoin:    onJoin,
		OnLeave:   onLeave,
	}

	sub, err := c.Subscribe(*channelPtr, events)
	if err != nil {
		log.Fatalln(fmt.Sprintf("Failed to subscribe: %s", err.Error()))
	}

	history, err := sub.History()
	if err != nil {
		log.Fatalln(fmt.Sprintf("Failed to get history: %s", err.Error()))
	}
	log.Printf("%d messages in channel %s history", len(history), sub.Channel())

	presence, err := sub.Presence()
	if err != nil {
		log.Fatalln(fmt.Sprintf("Failed to get presence: %s", err.Error()))
	}
	log.Printf("%d clients in channel %s", len(presence), sub.Channel())

	err = sub.Unsubscribe()
	if err != nil {
		log.Fatalln(fmt.Sprintf("Failed to unsubscribe: %s", err.Error()))
	}

	log.Printf("%s", time.Since(started))
}
