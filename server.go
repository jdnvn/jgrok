package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/gorilla/websocket"
)

const Port = "80"

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var clients = make(map[string]*Client)

func generateUniqueId() string {
	var id string
	for {
		id = strings.ToLower(strings.ReplaceAll(gofakeit.Adjective()+gofakeit.Animal(), " ", ""))
		_, exists := clients[id]
		if !exists {
			break
		}
	}
	return id
}

func httpError(w http.ResponseWriter, statusCode int, message string) {
	log.Printf("ERROR: %s - %s", statusCode, message)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(statusCode)
	errorResponse := map[string]string{
		"error": message,
	}
	json.NewEncoder(w).Encode(errorResponse)
}

func handler(w http.ResponseWriter, r *http.Request) {
	// check if this is a websocket request
	if websocket.IsWebSocketUpgrade(r) {
		// handle websocket upgrade
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

		err = ws.WriteMessage(websocket.TextMessage, []byte(id))
		if err != nil {
			log.Println("error sending client id:", err)
			return
		}
	} else {
		host := r.Host
		split_host := strings.Split(host, ".")
		if len(split_host) < 3 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		id := strings.ToLower(split_host[0])
		log.Println("incoming request to id:", id)

		// TODO: respond with JSON

		client, exists := clients[id]
		if !exists {
			httpError(w, http.StatusNotFound, "that jgrok URL does not exist!")
			return
		}

		// serialize and forward request to client
		body, err := io.ReadAll(r.Body)
		if err != nil {
			httpError(w, http.StatusBadRequest, "bad request!")
			log.Println("error reading incoming request body", err)
			return
		}
		forwardedReq := ForwardedRequest{
			Method:  r.Method,
			URL:     r.URL.String(),
			Headers: r.Header,
			Body:    body,
		}
		forwardedRequestJson, err := json.Marshal(forwardedReq)
		if err != nil {
			httpError(w, http.StatusBadRequest, "bad request!")
			log.Println("error marshaling request data:", err)
			return
		}

		log.Println("message to client:", string(forwardedRequestJson))

		err = client.conn.WriteMessage(websocket.TextMessage, forwardedRequestJson)
		if err != nil {
			httpError(w, http.StatusBadGateway, "could not reach server")
			log.Println("error forwarding request to client", err)
			return
		}

		_, msg, err := client.conn.ReadMessage()
		if err != nil {
			httpError(w, http.StatusNotFound, "that jgrok URL does not exist!")
			log.Println("error reading response from client:", err)
			client.conn.Close()
			delete(clients, id)
			return
		}

		log.Println("received reply from client, forwarding...")

		// deserialize the response from the client and respond with correct type and status code
		var forwardedResp ForwardedResponse
		err = json.Unmarshal(msg, &forwardedResp)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "internal server error :(")
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
		_, err = w.Write(forwardedResp.Body)
		if err != nil {
			log.Println("error forwarding response to caller", err)
			return
		}
	}
}

func purge_clients() {
	log.Println("purging clients...")
	for id, client := range clients {
		err := client.conn.Close()
		if err != nil {
			log.Printf("error closing connection for %s", id)
		}
	}
}

func startServer() {
	http.HandleFunc("/", handler)
	server := &http.Server{Addr: ":" + Port}

	// set up channel to listen for interrupt signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// run server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
		log.Printf("HTTP server started on port %s", Port)
	}()

	<-quit

	// handle shutdown
	log.Println("Shutting down server...")
	if err := server.Close(); err != nil {
		log.Fatalf("Error shutting down server: %v", err)
	}

	// clean up active websocket connections
	purge_clients()
}
