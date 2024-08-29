package main

import (
	"os"
    "os/signal"
	"syscall"
	"encoding/json"
	"io/ioutil"
    "log"
    "net/http"
	"strings"
    "github.com/gorilla/websocket"
	"github.com/matoous/go-nanoid/v2"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

type Client struct {
    conn *websocket.Conn
}

type ForwardedRequest struct {
    Method  string              `json:"method"`
    URL     string              `json:"url"`
    Headers map[string][]string `json:"headers"`
    Body    string              `json:"body"`
}

type ForwardedResponse struct {
    StatusCode int               `json:"status_code"`
    Headers    map[string][]string `json:"headers"`
    Body       string            `json:"body"`
}

var clients = make(map[string]*Client)
var broadcast = make(chan []byte)

// HTTP/WebSocket handler
func handler(w http.ResponseWriter, r *http.Request) {
    // Check if this is a WebSocket request
    if websocket.IsWebSocketUpgrade(r) {
        // Handle WebSocket upgrade and communication
        ws, err := upgrader.Upgrade(w, r, nil)
        if err != nil {
            log.Println("Upgrade:", err)
            return
        }

        // Register new WebSocket client
        client := &Client{conn: ws}
		nanoId, _ := gonanoid.New()
		id := strings.ToLower(nanoId)
        clients[id] = client
		log.Println("New client:", id)

		ws.WriteMessage(websocket.TextMessage, []byte(id))
    } else {
		host := r.Host
		split_host := strings.Split(host, ".")
		if (len(split_host) < 3) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		id := split_host[0]
		log.Println("Incoming request to id:", id)

		client, exists := clients[id]
		if (!exists) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("that jgrok URL does not exist"))
			return
		}

		// serialize and forward request to client
		body, err := ioutil.ReadAll(r.Body)
		forwardedReq := ForwardedRequest{
			Method:  r.Method,
			URL:     r.URL.String(),
			Headers: r.Header,
			Body:    string(body),
		}
		forwardedRequestJson, err := json.Marshal(forwardedReq)
		if err != nil {
			log.Println("Error marshaling request data:", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal server error :("))
			return
		}

		log.Println("message to client:", string(forwardedRequestJson))
		client.conn.WriteMessage(websocket.TextMessage, forwardedRequestJson)

		_, msg, err := client.conn.ReadMessage()
		if err != nil {
			log.Println("Error reading WebSocket message:", err)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("that jgrok URL does not exist"))
			client.conn.Close()
			delete(clients, id)
			return
		}
		log.Println("reply from client: ", string(msg))

		// deserialize the response from the client and respond with correct type and status code
		var forwardedResp ForwardedResponse
		err = json.Unmarshal(msg, &forwardedResp)
		if err != nil {
			log.Println("Error unmarshaling response data:", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal server error :("))
			return
		}

		// Set the HTTP response status and headers
		w.WriteHeader(forwardedResp.StatusCode)
		for key, values := range forwardedResp.Headers {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

		// set Content-Type explicitly based on response body type, default to json
		if forwardedResp.Headers["Content-Type"] == nil {
			w.Header().Set("Content-Type", "application/json")
		}
	
		// write the response body
		w.Write([]byte(forwardedResp.Body))
    }
}

func cleanup() {
	for id, client := range clients {
		err := client.conn.Close()
		if (err != nil) {
			log.Printf("Error closing connection for %s", id)
		}
	}
}

func main() {
	http.HandleFunc("/", handler)
	server := &http.Server{Addr: ":8080"}

	// set up channel to listen for interrupt signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// run server in a goroutine
	go func() {
		log.Println("HTTP server started on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// wait for interrupt signal
	<-quit

	// handle shutdown
	log.Println("Shutting down server...")
	if err := server.Close(); err != nil {
		log.Fatalf("Error shutting down server: %v", err)
	}

	// clean up websocket connections
	cleanup()
}
