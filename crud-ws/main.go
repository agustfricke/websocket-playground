package main

import (
  "flag"
  "log"
  "strings"
  "sync"

  "github.com/gofiber/fiber/v2"
  "github.com/gofiber/contrib/websocket"
  "github.com/go-redis/redis/v8"
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
  client *redis.Client
}

var messageStorage *messageStore

func initializeRedis() (*redis.Client, error) {
  client := redis.NewClient(&redis.Options{
    Addr: "localhost:6379", // Dirección de tu servidor Redis
    DB:   0,               // Número de base de datos en Redis
  })
  _, err := client.Ping(client.Context()).Result()
  return client, err
}

func runHub() {
  for {
    select {
    case connection := <-register:
      clients[connection] = &client{}
      log.Println("connection registered")

      messageStorage.Lock()
      messages, err := messageStorage.client.LRange(messageStorage.client.Context(), "messages", 0, -1).Result()
      if err == nil {
        for _, message := range messages {
          connection.WriteMessage(websocket.TextMessage, []byte(message))
        }
      }
      messageStorage.Unlock()

    case message := <-broadcast:
      log.Println("message received:", message)

      messageStorage.Lock()
      messageStorage.client.LPush(messageStorage.client.Context(), "messages", message)
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
      if err := messageStorage.client.LRem(messageStorage.client.Context(), "messages", 0, message).Err(); err != nil {
        log.Println("error deleting message:", err)
      }
      messageStorage.Unlock()

      // Envía un mensaje especial para indicar que se eliminó un mensaje
      for connection, c := range clients {
        go func(connection *websocket.Conn, c *client) {
          c.mu.Lock()
          defer c.mu.Unlock()
          if c.isClosing {
            return
          }
          if err := connection.WriteMessage(websocket.TextMessage, []byte("Deleted message: "+message)); err != nil {
            c.isClosing = true
            log.Println("write error:", err)

            connection.WriteMessage(websocket.CloseMessage, []byte{})
            connection.Close()
            unregister <- connection
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

  redisClient, err := initializeRedis()
  if err != nil {
    log.Fatal("Failed to connect to Redis:", err)
  }
  messageStorage = &messageStore{
    client: redisClient,
  }

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
        } else if strings.HasPrefix(messageStr, "edit ") {
          // Editar el mensaje
          editParts := strings.Fields(messageStr)
          if len(editParts) >= 3 {
            originalMessage := editParts[1]
            editedMessage := strings.Join(editParts[2:], " ")

            // Lógica para actualizar el mensaje en Redis
            messageStorage.Lock()
            if err := messageStorage.client.LRem(messageStorage.client.Context(), "messages", 0, originalMessage).Err(); err != nil {
              log.Println("error deleting original message:", err)
            } else {
              messageStorage.client.LPush(messageStorage.client.Context(), "messages", editedMessage)
              // Enviar el mensaje editado a todos los clientes
              broadcast <- "edit " + originalMessage + " " + editedMessage
            }
            messageStorage.Unlock()
          } else {
            log.Println("Invalid edit message format")
          }
        } else {
          broadcast <- messageStr
        }
      }
    }
  }))

  app.Get("/messages", func(c *fiber.Ctx) error {
    messageStorage.Lock()
    defer messageStorage.Unlock()
    messages, err := messageStorage.client.LRange(messageStorage.client.Context(), "messages", 0, -1).Result()
    if err != nil {
      log.Println("error retrieving messages:", err)
      return c.Status(fiber.StatusInternalServerError).JSON([]string{})
    }
    return c.JSON(messages)
  })

  addr := flag.String("addr", ":8080", "http service address")
  flag.Parse()
  log.Fatal(app.Listen(*addr))
}
