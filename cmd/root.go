package cmd

import (
	"fmt"
	"runtime"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"
)

type rootConfig struct {
	wsUrl                 string
	apiUrl                string
	secret                string
	channels              uint
	connectionsPerChannel uint
}

var RootConfig rootConfig

var rootCmd = &cobra.Command{
	Use:   "centrifugo-bench",
	Short: "Benchmark tools for Centrifugo",
	Long:  "",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		runtime.GOMAXPROCS(runtime.NumCPU()) // just to be sure :)

		GenerateApiUrl()

		GenerateChannelsNames()

		log.Printf("RootConfig: %+v", RootConfig)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&RootConfig.wsUrl, "ws-url", "", "WebSocket URL, e.g.: ws://localhost:80/connection/websocket")
	rootCmd.PersistentFlags().StringVar(&RootConfig.secret, "secret", "", "Secret")
	rootCmd.PersistentFlags().UintVar(&RootConfig.channels, "channels", 1, "Channels count")
	rootCmd.PersistentFlags().UintVar(&RootConfig.connectionsPerChannel, "connections-per-channel", 1, "Connections per channel")

	err := rootCmd.MarkPersistentFlagRequired("ws-url")
	if err != nil {
		log.Fatalf("MarkPersistentFlagRequired failed: %+v", err)
	}

	err = rootCmd.MarkPersistentFlagRequired("secret")
	if err != nil {
		log.Fatalf("MarkPersistentFlagRequired failed: %+v", err)
	}
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		log.Fatalf("Execute failed: %+v", err)
	}
}

func GenerateApiUrl() {
	u, err := url.Parse(RootConfig.wsUrl)
	if err != nil {
		log.Fatal("Failed to parse ws Url")
	}
	u.Query()
	hostname := u.Hostname()
	port := u.Port()
	if port == "" {
		RootConfig.apiUrl = fmt.Sprintf("http://%s/api/", hostname)
	} else {
		RootConfig.apiUrl = fmt.Sprintf("http://%s:%s/api/", hostname, port)
	}
}

var channels map[int]string

func GenerateChannelsNames() {
	channels = make(map[int]string)
	if RootConfig.channels == 1 {
		channels[0] = "bench"
	} else {
		for i := 0; i < int(RootConfig.channels); i++ {
			channels[i] = fmt.Sprintf("bench%d", i)
		}
	}
}

var osSignal chan os.Signal
var quitSignal chan bool

type benchMessage struct {
	Time    time.Time `json:"time"`
	Payload string    `json:"payload"`
}
