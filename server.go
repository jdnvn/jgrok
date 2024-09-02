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
	"github.com/brianvoe/gofakeit/v6"
)

const port = "80"

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

var clients = make(map[string]*Client)
var broadcast = make(chan []byte)

func generateUniqueId() string {
	var id string
    for {
        id = strings.ToLower(strings.ReplaceAll(gofakeit.Adjective() + gofakeit.Animal(), " ", ""))
		_, exists := clients[id]
        if !exists {
            break
        }
    }
	return id
}

func handler(w http.ResponseWriter, r *http.Request) {
    // check if this is a websocket request
    if websocket.IsWebSocketUpgrade(r) {
        // handle websocket upgrade and communication
        ws, err := upgrader.Upgrade(w, r, nil)
        if err != nil {
            log.Println("error creating websocket connection:", err)
            return
        }

        // register new websocket client
        client := &Client{conn: ws}

		// generate unique id to be used as the subdomain and map it to the client's websocket connection
		id := generateUniqueId()
        clients[id] = client
		log.Println("new client:", id)

		ws.WriteMessage(websocket.TextMessage, []byte(id))
    } else {
		host := r.Host
		split_host := strings.Split(host, ".")
		if len(split_host) < 3 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		id := strings.ToLower(split_host[0])
		log.Println("Incoming request to id:", id)

		// TODO: respond with JSON

		client, exists := clients[id]
		if !exists {
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

func purge_clients() {
	for id, client := range clients {
		err := client.conn.Close()
		if err != nil {
			log.Printf("Error closing connection for %s", id)
		}
	}
}

func main() {
	http.HandleFunc("/", handler)
	server := &http.Server{Addr: ":" + port}

	// set up channel to listen for interrupt signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// run server in a goroutine
	go func() {
		log.Printf("HTTP server started on port %s", port)
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
	purge_clients()
}
