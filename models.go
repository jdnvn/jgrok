package main

import (
	"github.com/gorilla/websocket"
)

type Client struct {
	conn *websocket.Conn
}

type ForwardedRequest struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
}

type ForwardedResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
}
