package main

import (
	"bytes"
	"log"
	"net/url"
	"os"
	"os/signal"
	"time"
	"io/ioutil"
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"
)

func main() {
	args := os.Args

	port := ""
	if (len(args) > 1) {
		port = args[1]
	} else {
		log.Fatal("You must provide the port of a local running HTTP server")
		return
	}

	local_server_url := "http://localhost:" + port

	// connect to the websocket server
	host := "kimiko.me:8080"
	u := url.URL{Scheme: "ws", Host: host}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("Dial error:", err)
	}
	defer c.Close()

	// setup a channel to catch interrupt signals for clean shutdown
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// read ID and print to user
	_, id, err := c.ReadMessage()
	log.Printf("%s.%s -> %s", string(id), host, local_server_url)

	client := &http.Client{}

	// start listening for messages from socket
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("Read error:", err)
				return
			}

			log.Printf("Received: %s, forwarding to local server", message)

			var forwardedReq ForwardedRequest
			parseErr := json.Unmarshal(message, &forwardedReq)
			if (parseErr != nil) {
				log.Println("Error parsing request:", parseErr)
			} else {
				bodyReader := bytes.NewReader([]byte(forwardedReq.Body))
				req, _ := http.NewRequest(forwardedReq.Method, local_server_url + forwardedReq.URL, bodyReader)

				for key, values := range forwardedReq.Headers {
					for _, value := range values {
						req.Header.Add(key, value)
					}
				}

				resp, _ := client.Do(req)
				body, _ := ioutil.ReadAll(resp.Body)

				responseMap := map[string]interface{}{
					"status_code": resp.StatusCode,
					"headers":     resp.Header,
					"body":        string(body),
				}

				responseJson, _ := json.MarshalIndent(responseMap, "", "  ")

				c.WriteMessage(websocket.TextMessage, []byte(responseJson))
			}
		}
	}()

	// keep the connection alive until an interrupt is received
	for {
		select {
		case <-done:
			return
		case <-interrupt:
			log.Println("Interrupt received, closing connection...")

			// close the websocket connection gracefully
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("Close error:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
