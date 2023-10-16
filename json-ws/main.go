package main

import (
    "encoding/json"
    "flag"
    "log"
    "sync"

    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/contrib/websocket"
)

type Message struct {
    Text string `json:"text"`
    Title string `json:"title"`
}

type client struct {
    isClosing bool
    mu        sync.Mutex
}

var clients = make(map[*websocket.Conn]*client)
var register = make(chan *websocket.Conn)
var broadcast = make(chan Message) // Change the type to Message
var unregister = make(chan *websocket.Conn)

func runHub() {
    for {
        select {
        case connection := <-register:
            clients[connection] = &client{}
            log.Println("connection registered")

        case message := <-broadcast:
            log.Println("message received:", message.Text)
            log.Println("title received:", message.Title)
            messageJSON, err := json.Marshal(message)
            if err != nil {
                log.Println("json marshaling error:", err)
                continue
            }
            for connection, c := range clients {
                go func(connection *websocket.Conn, c *client) {
                    c.mu.Lock()
                    defer c.mu.Unlock()
                    if c.isClosing {
                        return
                    }
                    if err := connection.WriteMessage(websocket.TextMessage, messageJSON); err != nil {
                        c.isClosing = true
                        log.Println("write error:", err)

                        connection.WriteMessage(websocket.CloseMessage, []byte{})
                        connection.Close()
                        unregister <- connection
                    }
                }(connection, c)
            }

        case connection := <-unregister:
            delete(clients, connection)

            log.Println("connection unregistered")
        }
    }
}

func main() {
    app := fiber.New()

    app.Static("/", "./home.html")

    app.Use(func(c *fiber.Ctx) error {
        if websocket.IsWebSocketUpgrade(c) {
            return c.Next()
        }
        return c.SendStatus(fiber.StatusUpgradeRequired)
    })

    go runHub()

    app.Get("/ws", websocket.New(func(c *websocket.Conn) {
        defer func() {
            unregister <- c
            c.Close()
        }()

        register <- c

        for {
            messageType, message, err := c.ReadMessage()
            if err != nil {
                if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
                    log.Println("read error:", err)
                }

                return
            }

            if messageType == websocket.TextMessage {
                var msg Message
                if err := json.Unmarshal(message, &msg); err != nil {
                    log.Println("json unmarshaling error:", err)
                    continue
                }
                broadcast <- msg
            } else {
                log.Println("websocket message received of type", messageType)
            }
        }
    }))

    addr := flag.String("addr", ":8080", "http service address")
    flag.Parse()
    log.Fatal(app.Listen(*addr))
}
