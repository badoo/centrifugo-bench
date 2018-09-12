package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/centrifugal/centrifuge-go"
	"github.com/centrifugal/centrifugo/libcentrifugo/auth"
	"github.com/spf13/cobra"
)

var subscribeCmd = &cobra.Command{
	Use:   "subscribe",
	Short: "Subscribe to Centrifugo and listening for messages",
	Long:  "",
	Run: subscribeRun,
}

func init() {
	rootCmd.AddCommand(subscribeCmd)
}

func subscribeRun (cmd *cobra.Command, args []string) {
	log.Printf("Subscribing")

	go CollectDeliveryTime()

	for i := 0; i < int(RootConfig.channels); i++ {
		for j := 0; j < int(RootConfig.connectionsPerChannel); j++ {
			time.Sleep(time.Millisecond)
			CreateNewSubscribeConnection(i, j)
		}
	}

	osSignal = make(chan os.Signal)
	quitSignal = make(chan bool)

	signal.Notify(osSignal, syscall.SIGTERM)
	signal.Notify(osSignal, syscall.SIGINT)

	go func() {
		<-osSignal
		close(quitSignal)
	}()

	go PrintSubscriberRealTimeStat()

	<-quitSignal
	PrintSubscriberTotalStats()
}

var messagesReceived int64 = 0
var subscribeConnectionsEstablished int64 = 0

var deliveryTimeChannel chan int
var deliveryTimeMap map[int] int

func CreateNewSubscribeConnection(channel int, client int) {
	user := channel * int(RootConfig.connectionsPerChannel) + client
	credentials := GenerateSubscribeCredentials(user)

	var reconnectStrategy = &centrifuge.BackoffReconnect{
		NumReconnect: 5,
		Min:          100 * time.Millisecond,
		Max:          10 * time.Second,
		Factor:       2,
		Jitter:       true,
	}

	events := &centrifuge.EventHandler{
		OnDisconnect: func(c centrifuge.Centrifuge) error {
			log.Println("Disconnected")
			err := c.Reconnect(reconnectStrategy)
			if err != nil {
				log.Println(fmt.Sprintf("Failed to reconnect: %s", err.Error()))
				atomic.AddInt64(&subscribeConnectionsEstablished, -1)
			} else {
				log.Println("Reconnected")
			}
			return nil
		},
	}

	conf := centrifuge.DefaultConfig
	conf.Timeout = 10 * time.Second

	c := centrifuge.NewCentrifuge(RootConfig.wsUrl, credentials, events, conf)

	err := c.Reconnect(reconnectStrategy)
	if err != nil {
		log.Fatalln(fmt.Sprintf("Failed to connect: %s", err.Error()))
	}

	atomic.AddInt64(&subscribeConnectionsEstablished, 1)

	subEvents := &centrifuge.SubEventHandler{
		OnMessage: func(sub centrifuge.Sub, msg centrifuge.Message) error {
			var BenchMessage benchMessage
			err := json.Unmarshal(*msg.Data, &BenchMessage)
			if err != nil {
				log.Println(fmt.Sprintf("Failed to unmarshal message '%s': %+v", string(msg.Data[:]), err))
				return nil
			}

			deliveryTime := int(time.Since(BenchMessage.Time).Nanoseconds() / 1000000)
			//log.Println(fmt.Sprintf("BenchMessage: %+v, ms: %d", BenchMessage, deliveryTime))
			deliveryTimeChannel <- deliveryTime

			atomic.AddInt64(&messagesReceived, 1)

			return nil
		},
	}

	sub, err := c.Subscribe(channels[channel], subEvents)
	if err != nil {
		log.Fatalln(fmt.Sprintf("Failed to subscribe to channel %s: %s", channels[channel], err.Error()))
	}

	msgs, err := sub.History()
	if err != nil {
		log.Fatalln(fmt.Sprintf("Error retreiving channel history: %s", err.Error()))
	} else {
		log.Printf("Connection established: client #%d connected (%d messages in channel history)", user, len(msgs))
	}
}

func GenerateSubscribeCredentials(user int) *centrifuge.Credentials {
	// User ID
	user_str := strconv.Itoa(user)

	// Current timestamp as string.
	timestamp := centrifuge.Timestamp()

	// Empty info.
	info := ""

	// Generate client token so Centrifugo server can trust connection parameters received from client.
	token := auth.GenerateClientToken(RootConfig.secret, user_str, timestamp, info)

	return &centrifuge.Credentials{
		User:      user_str,
		Timestamp: timestamp,
		Info:      info,
		Token:     token,
	}
}

func CollectDeliveryTime() {
	deliveryTimeChannel = make(chan int, RootConfig.channels * RootConfig.connectionsPerChannel)
	deliveryTimeMap =  make(map[int]int)

	var deliveryTime int

	for {
		select {
		case <-quitSignal:
		case deliveryTime = <- deliveryTimeChannel:
			deliveryTimeMap[deliveryTime]++
			continue
		}
		break
	}
}

func PrintSubscriberRealTimeStat() {
	var statTicker = time.NewTicker(time.Second)
	var prevMessagesReceived int64 = 0
	for {
		select {
		case <-quitSignal:
		case <-statTicker.C:
			currMessagesReceived := atomic.LoadInt64(&messagesReceived)
			currClientsConnected := atomic.LoadInt64(&subscribeConnectionsEstablished)
			log.Printf(
				"Messages received: %d total,\t%d per second,\t%d per connection per second,\t%d connections established",
				currMessagesReceived,
				currMessagesReceived-prevMessagesReceived,
				int(float32(currMessagesReceived-prevMessagesReceived) / float32(RootConfig.connectionsPerChannel)),
				currClientsConnected)
			prevMessagesReceived = currMessagesReceived
			continue
		}
		break
	}
}

func PrintSubscriberTotalStats() {
	keys := make([]int, 0, len(deliveryTimeMap))
	totalRequests := 0
	for k := range deliveryTimeMap {
		keys = append(keys, k)
		// fuck floats
		deliveryTimeMap[k] *= 100
		totalRequests += deliveryTimeMap[k]
	}
	if len(keys) == 0 {
		return
	}
	sort.Ints(keys)
	percentile := 5
	currentKey := 0
	currentTime := keys[currentKey]
	currentCount := deliveryTimeMap[currentTime]

	fmt.Println("")
	fmt.Println("-----------------------------")
	fmt.Printf("Total Requests:\t%d\n", totalRequests / 100)
	fmt.Println("-----------------------------")

	for {
		if percentile > 100 {
			break
		}
		if currentCount >= (percentile * (totalRequests / 100)) {
			fmt.Printf("p%-3d\t%d ms\n", percentile, currentTime)
			percentile += 5
			continue
		}
		currentKey += 1
		currentTime = keys[currentKey]
		currentCount += deliveryTimeMap[currentTime]
	}

	time.Sleep(time.Second)
	os.Exit(0)
}
