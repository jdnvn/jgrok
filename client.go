package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

const PublicServerHost = "lvh.me"

func main() {
	args := os.Args

	localPort := ""
	if len(args) > 1 {
		localPort = args[1]
	} else {
		log.Fatal("You must provide the port of a local running HTTP server")
		return
	}

	// TODO: verify port/if server is running on that port

	localServerHost := "http://localhost:" + localPort

	// connect to the websocket server
	u := url.URL{Scheme: "ws", Host: PublicServerHost}
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
	if err != nil {
		log.Println("error receiving id from server", err)
		return
	}
	log.Printf("%s.%s -> %s", string(id), PublicServerHost, localServerHost)

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

			log.Printf("Received: %s, forwarding to local server...", message)

			var forwardedReq ForwardedRequest
			parseErr := json.Unmarshal(message, &forwardedReq)
			if parseErr != nil {
				log.Println("Error parsing request:", parseErr)
			} else {
				bodyReader := bytes.NewReader([]byte(forwardedReq.Body))
				req, _ := http.NewRequest(forwardedReq.Method, localServerHost+forwardedReq.URL, bodyReader)

				for key, values := range forwardedReq.Headers {
					for _, value := range values {
						req.Header.Add(key, value)
					}
				}

				resp, _ := client.Do(req)
				body, _ := io.ReadAll(resp.Body)

				forwardedResp := ForwardedResponse{
					StatusCode: resp.StatusCode,
					Headers:    resp.Header,
					Body:       body,
				}

				responseJson, err := json.Marshal(forwardedResp)
				if err != nil {
					log.Println("Error marshaling response:", err)
					return
				}

				err = c.WriteMessage(websocket.TextMessage, responseJson)
				if err != nil {
					log.Println("Error writing response to socket:", err)
					return
				}
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
