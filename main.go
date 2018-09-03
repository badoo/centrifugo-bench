package main

// Connect, subscribe on channel, publish into channel, read presence and history info.

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/centrifugal/centrifuge-go"
	"github.com/centrifugal/centrifugo/libcentrifugo/auth"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

var SafeClient = &http.Client{
	Timeout: time.Second * 5,
}

type config struct {
	secret                string
	wsUrl                 string
	channels              uint
	clientsPerChannel     uint
	connectionConcurrency uint
	channelRps            uint
}

type publishMessage struct {
	Channel   string `json:"channel"`
	Data      interface{} `json:"data"`
}

type rawMessage struct {
	Method   string `json:"method"`
	Params   interface{} `json:"params"`
}

type connectionParams struct {Channel int; Client int}

var Config config

var channels map[int]string

var msgSent int64 = 0
var msgReceived int64 = 0
var clientsConnected int64 = 0

var stats chan int
var osSignal chan os.Signal
var quitSignal chan bool
var tasks chan string
var createConnection chan connectionParams
var requestsTable map[int] int

var startTime time.Time

func parseFlags () {
	flag.StringVar(&Config.secret, "secret", "", "Secret.")
	flag.StringVar(&Config.wsUrl, "url", "", "WS URL, e.g.: ws://localhost:8000/connection/websocket.")
	flag.UintVar(&Config.channels, "channels", 1, "Channels count.")
	flag.UintVar(&Config.clientsPerChannel, "clients-per-channel", 1, "Clients per channel count.")
	flag.UintVar(&Config.connectionConcurrency, "connection-concurrency", 10, "Max concurrency for establishing connections")
	flag.UintVar(&Config.channelRps, "channel-rps", 1, "Message per second for channel")

	flag.Parse()
}

func generateChannelsNames() {
	channels = make(map[int]string)
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
	userStr := strconv.Itoa(user)

	// Current timestamp as string.
	timestamp := centrifuge.Timestamp()

	// Empty info.
	info := ""

	// Generate client token so Centrifugo server can trust connection parameters received from client.
	token := auth.GenerateClientToken(Config.secret, userStr, timestamp, info)

	return &centrifuge.Credentials{
		User:      userStr,
		Timestamp: timestamp,
		Info:      info,
		Token:     token,
	}
}

func newConnection(channel int, client int) {
	creds := credentials(channel * int(Config.clientsPerChannel) + client)

	var backoffReconnect = &centrifuge.BackoffReconnect{
		NumReconnect: 5,
		Min:          100 * time.Millisecond,
		Max:          10 * time.Second,
		Factor:       2,
		Jitter:       true,
	}

	events := &centrifuge.EventHandler{
		OnDisconnect: func(c centrifuge.Centrifuge) error {
			log.Println("Disconnected")
			err := c.Reconnect(backoffReconnect)
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

	err := c.Reconnect(backoffReconnect)
	if err != nil {
		log.Fatalln(fmt.Sprintf("Failed to connect: %s", err.Error()))
	}

	atomic.AddInt64(&clientsConnected, 1)

	subEvents := &centrifuge.SubEventHandler{
		OnMessage: func(sub centrifuge.Sub, msg centrifuge.Message) error {

			var unpackedTime time.Time
			json.Unmarshal(*msg.Data, &unpackedTime)
			roundTrip := int(time.Since(unpackedTime).Nanoseconds() / 1000000)
			stats <- roundTrip
			atomic.AddInt64(&msgReceived, 1)
			return nil
		},
	}

	_, err = c.Subscribe(channels[channel], subEvents)
	if err != nil {
		log.Fatalln(fmt.Sprintf("Failed to subscribe to channel %s: %s", channels[channel], err.Error()))
	}
}

func createConnectionWorker(done chan bool) {
	var params connectionParams
	var ok bool
	for {
		select {
		case <-quitSignal:
		case params, ok = <-createConnection:
			if !ok {
				done <- true
				return
			}
			newConnection(params.Channel, params.Client)
			continue
		}
		done <- true
		break
	}
}

func createConnectionsPool() {
	totalConnections := int(Config.channels * Config.clientsPerChannel)
	stats = make(chan int, totalConnections)
	createConnection = make(chan connectionParams, Config.channels * Config.clientsPerChannel)

	for i := 0; i < int(Config.channels); i++ {
		for j := 0; j < int(Config.clientsPerChannel); j++ {
			createConnection <- connectionParams{i, j}
		}
	}

	close(createConnection)

	numWorkers := int(Config.connectionConcurrency)
	if numWorkers > totalConnections {
		numWorkers = totalConnections
	}
	connectionsDone := make(chan bool)
	for i := 0; i < numWorkers; i++ {
		go createConnectionWorker(connectionsDone)
	}
	for i := 0; i < numWorkers; i++ {
		<-connectionsDone
	}
}
func getApiUrl() string {
	u, err := url.Parse(Config.wsUrl)
	if err != nil {
		log.Fatal("Failed to parse ws Url")
	}
	u.Query()
	hostname := u.Hostname()
	port := u.Port()
	var apiUrl string
	if port == "" {
		apiUrl = fmt.Sprintf("http://%s/api/", hostname)
	} else {
		apiUrl = fmt.Sprintf("http://%s:%s/api/", hostname, port)
	}
	return apiUrl
}

func rawRequest(method string, params interface{}) (body string, err error)  {
	publishMsg := &rawMessage{
		Method:    method,
		Params:    params,
	}

	dataBytes, err := json.Marshal(publishMsg)

	if err != nil {
		log.Fatalln(err)
	}

	data := string(dataBytes)

	q := make(url.Values)
	q.Set("data", data)
	q.Set("sign", auth.GenerateApiSign(Config.secret, dataBytes))

	resp, err := SafeClient.PostForm(getApiUrl(), q)

	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(bodyBytes), nil
}

func sendMessage(channel string) {
	jitter := time.Duration(rand.Intn(500)) * time.Millisecond
	time.Sleep(jitter)

	publishMsg := &publishMessage{
		Channel: channel,
		Data: time.Now(),
	}
	_, err := rawRequest("publish", publishMsg)
	if err != nil {
		log.Printf("Failed to send message to channel: %s\n", err.Error())
	}
}

func sendMessageWorker() {

	for {
		select {
		case <-quitSignal:
		case channel := <-tasks:
			sendMessage(channel)
			continue
		}
		break
	}
}

func sendMessageLoop(t *time.Ticker) {
	for {
		select {
		case <-quitSignal:
		case <-t.C:
			if len(tasks) > 0 {
				log.Fatalf("Send message queue overflow: %d", len(tasks))
			}
			for i := 0; i < int(Config.channelRps); i++ {
				for _, channel := range channels {
					tasks <- channel
				}
			}
			atomic.AddInt64(&msgSent, int64(Config.channels))
			continue
		}
		break
	}
}

func startMessagesSending() {
	sendMessageWorkers := int(Config.channels * Config.channelRps)
	tasks = make(chan string, sendMessageWorkers)

	ticker := time.NewTicker(time.Second)
	go sendMessageLoop(ticker)

	for i := 0; i < sendMessageWorkers; i++ {
		go sendMessageWorker()
	}
}

func printRealtimeStat(t *time.Ticker) {
	var prevMsgReceived int64 = 0
	for {
		select {
		case <-quitSignal:
		case <-t.C:
			currMsgSent := atomic.LoadInt64(&msgSent)
			currMsgReceived := atomic.LoadInt64(&msgReceived)
			currClientsConnected := atomic.LoadInt64(&clientsConnected)
			log.Printf(
				"Messages sent: %d received: %d total,\t%d per second,\t%d per client per second,\t%d clients connected",
				currMsgSent,
				currMsgReceived,
				currMsgReceived - prevMsgReceived,
				int(float32(currMsgReceived - prevMsgReceived) / float32(Config.clientsPerChannel)),
				currClientsConnected)
			prevMsgReceived = currMsgReceived
			continue
		}
		break
	}
}

func collectStats() {
	var roundTrip int
	requestsTable =  make(map[int]int)
	for {
		select {
		case <-quitSignal:
		case roundTrip = <- stats:
			requestsTable[roundTrip]++
			continue
		}
		break
	}
}

func printStats() {
	keys := make([]int, 0, len(requestsTable))
	totalRequests := 0
	for k := range requestsTable {
		keys = append(keys, k)
		// fuck floats
		requestsTable[k] *= 100
		totalRequests += requestsTable[k]
	}
	if len(keys) == 0 {
		return
	}
	sort.Ints(keys)
	percentile := 5
	currentKey := 0
	currentTime := keys[currentKey]
	currentCount := requestsTable[currentTime]

	totalTime := time.Since(startTime)
	avgRps := float64(totalRequests) / 100 / totalTime.Seconds()
	rpsPerClient := avgRps / float64(Config.clientsPerChannel)

	fmt.Print("\n\n")
	fmt.Printf("Channels:\t%d\n", Config.channels)
	fmt.Printf("Clients per Channel:\t%d\n", Config.clientsPerChannel)
	fmt.Printf("RPS per Channel:\t%d\n", Config.channelRps)
	fmt.Println("-----------------------------")
	fmt.Printf("Total time:\t%s\n", totalTime.Round(time.Second))
	fmt.Printf("Total Requests:\t%d\n", totalRequests / 100)
	fmt.Printf("Avg RPS:\t%.2f\n", avgRps)
	fmt.Printf("Per Client RPS:\t%.2f\n", rpsPerClient)

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
		currentCount += requestsTable[currentTime]
	}

	time.Sleep(time.Second)
	os.Exit(0)
}


func main() {
	runtime.GOMAXPROCS(runtime.NumCPU()) // just to be sure :)

	osSignal = make(chan os.Signal)
	quitSignal = make(chan bool)

	signal.Notify(osSignal, syscall.SIGTERM)
	signal.Notify(osSignal, syscall.SIGINT)

	go func() {
		<-osSignal
		close(quitSignal)
	}()

	statTicker := time.NewTicker(time.Second)
	go printRealtimeStat(statTicker)


	parseFlags()
	generateChannelsNames()

	createConnectionsPool()

	startTime = time.Now()
	go collectStats()

	startMessagesSending()

	<-quitSignal
	printStats()
}
