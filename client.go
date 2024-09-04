package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gorilla/websocket"
	"github.com/rivo/tview"
)

const PublicServerHost = "lvh.me"

type UI struct {
	app   *tview.Application
	table *tview.Table
	rows  int
}

func createUI(id string, localServerHost string) *UI {
	app := tview.NewApplication()

	title := tview.NewTextView()
	title.SetText(" jgrok")
	title.SetTextColor(tcell.ColorDarkCyan)

	forwarding := tview.NewTextView()
	forwarding.SetText(fmt.Sprintf(" http://%s.%s [yellow]->[-] %s", string(id), PublicServerHost, localServerHost))
	forwarding.SetDynamicColors(true)

	spacer := tview.NewTextView()

	table := tview.NewTable()

	requests := tview.NewFlex()
	requests.SetTitle("requests")
	requests.SetTitleColor(tcell.ColorLightPink)
	requests.SetTitleAlign(tview.AlignLeft)
	requests.SetDirection(tview.FlexRow)
	requests.SetBorder(true)
	requests.AddItem(spacer, 1, 0, false)
	requests.AddItem(table, 0, 1, false)

	container := tview.NewFlex()
	container.SetDirection(tview.FlexRow)
	container.AddItem(title, 1, 0, false)
	container.AddItem(spacer, 1, 0, false)
	container.AddItem(forwarding, 1, 0, false)
	container.AddItem(spacer, 1, 0, false)
	container.AddItem(requests, 0, 2, false)

	app.SetRoot(container, true)

	ui := UI{app: app, table: table, rows: 0}

	return &ui
}

func (ui *UI) addTableRow(httpMethod string, path string, statusCode int) {
	ui.table.SetCell(ui.rows, 0, tview.NewTableCell(time.Now().Format(" 15:04:05.456")))
	ui.table.SetCell(ui.rows, 1, tview.NewTableCell(httpMethod))
	ui.table.SetCell(ui.rows, 2, tview.NewTableCell(path))
	statusCodeCell := tview.NewTableCell(strconv.Itoa(statusCode))
	if statusCode >= 400 {
		statusCodeCell.SetTextColor(tcell.ColorIndianRed)
	}
	ui.table.SetCell(ui.rows, 3, statusCodeCell)
	ui.app.Draw()
	ui.rows += 1
}

func connectToServer() (*websocket.Conn, error) {
	u := url.URL{Scheme: "ws", Host: PublicServerHost}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	return conn, err
}

func deserializeMessage(message []byte) (*ForwardedRequest, error) {
	var forwardedReq ForwardedRequest
	err := json.Unmarshal(message, &forwardedReq)
	return &forwardedReq, err
}

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
	conn, err := connectToServer()
	if err != nil {
		log.Fatal("error connecting to jgrok server", err)
	}

	// read ID to be used as the subdomain
	_, id, err := conn.ReadMessage()
	if err != nil {
		log.Fatal("error connecting to jgrok server", err)
	}
	defer conn.Close()

	client := &http.Client{}

	ui := createUI(string(id), localServerHost)

	// setup channels for clean shutdown
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	done := make(chan struct{})

	// start the ui in its own thread
	go func() {
		defer close(done)

		if err := ui.app.Run(); err != nil {
			panic(err)
		}
	}()

	// start listening for messages from websocket
	go func() {
		defer close(done)

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Println("Read error:", err)
				continue
			}

			forwardedReq, err := deserializeMessage(message)
			if err != nil {
				log.Println("Error deserializing message:", err)
				continue
			}

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

			err = conn.WriteMessage(websocket.TextMessage, responseJson)
			if err != nil {
				log.Println("Error writing response to socket:", err)
				return
			}

			ui.addTableRow(forwardedReq.Method, forwardedReq.URL, forwardedResp.StatusCode)
		}
	}()

	// keep the connection alive until an interrupt is received
	for {
		select {
		case <-done:
			ui.app.Stop()
			return
		case <-interrupt:
			ui.app.Stop()

			// close the websocket connection gracefully
			err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Fatal("Close error:", err)
			}

			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
