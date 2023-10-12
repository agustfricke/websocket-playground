package main

import (
	"flag"
	"log"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/contrib/websocket"
)

type client struct {
	isClosing bool
	mu        sync.Mutex
}

var clients = make(map[*websocket.Conn]*client)
var register = make(chan *websocket.Conn)
var unregister = make(chan *websocket.Conn)

var broadcast = make(chan string)
var deleteMessage = make(chan string)

type messageStore struct {
	sync.Mutex
	messages []string
}

var messageStorage = &messageStore{
	messages: make([]string, 0),
}

func runHub() {
	for {
		select {
		case connection := <-register:
			clients[connection] = &client{}
			log.Println("connection registered")

			messageStorage.Lock()
			for _, message := range messageStorage.messages {
				connection.WriteMessage(websocket.TextMessage, []byte(message))
			}
			messageStorage.Unlock()

		case message := <-broadcast:
			log.Println("message received:", message)

			messageStorage.Lock()
			messageStorage.messages = append(messageStorage.messages, message)
			messageStorage.Unlock()

			for connection, c := range clients {
				go func(connection *websocket.Conn, c *client) {
					c.mu.Lock()
					defer c.mu.Unlock()
					if c.isClosing {
						return
					}
					if err := connection.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
						c.isClosing = true
						log.Println("write error:", err)

						connection.WriteMessage(websocket.CloseMessage, []byte{})
						connection.Close()
						unregister <- connection
					}
				}(connection, c)
			}

		case message := <-deleteMessage:
			log.Println("message to delete:", message)

			messageStorage.Lock()
			updatedMessages := make([]string, 0)
			deletedMessage := ""
			for _, msg := range messageStorage.messages {
				if msg == message {
					deletedMessage = msg
				} else {
					updatedMessages = append(updatedMessages, msg)
				}
			}
			messageStorage.messages = updatedMessages
			messageStorage.Unlock()

			// Envía un mensaje especial para indicar que se eliminó un mensaje
			for connection, c := range clients {
				go func(connection *websocket.Conn, c *client) {
					c.mu.Lock()
					defer c.mu.Unlock()
					if c.isClosing {
						return
					}
					if deletedMessage != "" {
						if err := connection.WriteMessage(websocket.TextMessage, []byte("Deleted message: "+deletedMessage)); err != nil {
							c.isClosing = true
							log.Println("write error:", err)

							connection.WriteMessage(websocket.CloseMessage, []byte{})
							connection.Close()
							unregister <- connection
						}
					}
				}(connection, c)
			}
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
				messageStr := string(message)
				log.Println("websocket message received:", messageStr)

				if strings.HasPrefix(messageStr, "delete ") {
					// Eliminar el mensaje con nombre específico
					messageToBeDeleted := strings.TrimPrefix(messageStr, "delete ")
					deleteMessage <- messageToBeDeleted
				} else {
					broadcast <- messageStr
				}
			}
		}
	}))

	app.Get("/messages", func(c *fiber.Ctx) error {
		messageStorage.Lock()
		defer messageStorage.Unlock()
		return c.JSON(messageStorage.messages)
	})

	addr := flag.String("addr", ":8080", "http service address")
	flag.Parse()
	log.Fatal(app.Listen(*addr))
}
