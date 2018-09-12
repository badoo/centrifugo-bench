package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"sync/atomic"
	"time"

	"github.com/centrifugal/centrifugo/libcentrifugo/auth"
	"github.com/spf13/cobra"
	"net/http"
)

type publishConfig struct {
	messagesPerSecondPerConnectionPerChannel uint
}

var PublishConfig publishConfig

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish messages to Centrifugo",
	Long:  "",
	Run: publishRun,
}

func init() {
	publishCmd.Flags().UintVar(&PublishConfig.messagesPerSecondPerConnectionPerChannel, "mps", 1, "Message per second per connection per channel")

	rootCmd.AddCommand(publishCmd)
}

func publishRun (cmd *cobra.Command, args []string) {
	log.Printf("Publishing")

	for i := 0; i < int(RootConfig.channels); i++ {
		for j := 0; j < int(RootConfig.connectionsPerChannel); j++ {
			time.Sleep(time.Millisecond)
			CreateNewPublishConnection(i, j)
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

	go PrintPublisherRealTimeStat()

	<-quitSignal
	PrintPublisherTotalStats()
}

var messagesPublished int64 = 0

func CreateNewPublishConnection(channel int, client int) {
	ticker := time.NewTicker(time.Duration(uint(time.Second) / PublishConfig.messagesPerSecondPerConnectionPerChannel))
	go func(channel int, client int, ticker *time.Ticker) {
		for {
			select {
			case <-quitSignal:
			case <-ticker.C:
				PublishMessage(channels[channel])
				continue
			}
			break
		}
	}(channel, client, ticker)
}

type publishMessage struct {
	Channel   string      `json:"channel"`
	Data      interface{} `json:"data"`
}

func PublishMessage(channel string) {
	benchMsg := &benchMessage{
		Time: time.Now(),
		Payload: "payload",
	}

	publishMsg := &publishMessage{
		Channel: channel,
		Data: benchMsg,
	}

	_, err := MakeApiRequest("publish", publishMsg)
	if err != nil {
		log.Println(fmt.Sprintf("Failed to publish message to channel '%s': %+v", channel, err))
	}

	atomic.AddInt64(&messagesPublished, 1)
}

type apiMessage struct {
	Method   string      `json:"method"`
	Params   interface{} `json:"params"`
}

var SafeClient = &http.Client{
	Timeout: time.Second * 5,
}

func MakeApiRequest(method string, params interface{}) (body string, err error)  {
	apiMsg := &apiMessage{
		Method:    method,
		Params:    params,
	}

	dataBytes, err := json.Marshal(apiMsg)
	if err != nil {
		log.Fatalln(err)
	}

	data := string(dataBytes)

	q := make(url.Values)
	q.Set("data", data)
	q.Set("sign", auth.GenerateApiSign(RootConfig.secret, dataBytes))

	resp, err := SafeClient.PostForm(RootConfig.apiUrl, q)
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

func PrintPublisherRealTimeStat() {
	var statTicker = time.NewTicker(time.Second)
	var prevMessagesPublished int64 = 0
	for {
		select {
		case <-quitSignal:
		case <-statTicker.C:
			currMessagesPublished := atomic.LoadInt64(&messagesPublished)
			log.Printf(
				"Messages published: %d total,\t%d per second,\t%d per channel per second",
				currMessagesPublished,
				currMessagesPublished-prevMessagesPublished,
				int(float32(currMessagesPublished-prevMessagesPublished) / float32(RootConfig.channels)))
			prevMessagesPublished = currMessagesPublished
			continue
		}
		break
	}
}

func PrintPublisherTotalStats() {
	time.Sleep(time.Second)
	os.Exit(0)
}