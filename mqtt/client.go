package mqtt

import (
	"fmt"
	"log/slog"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

// MessageHandler is called when a parsed MQTT message is received.
type MessageHandler func(carID string, field TopicField, value string)

// Client wraps the Paho MQTT client with auto-reconnect and topic subscriptions.
type Client struct {
	client  pahomqtt.Client
	carIDs  []string
	handler MessageHandler
	logger  *slog.Logger
}

// NewClient creates a new MQTT client. Call Connect() to start.
func NewClient(host, port, username, password, clientID string, carIDs []string, handler MessageHandler, logger *slog.Logger) *Client {
	c := &Client{
		carIDs:  carIDs,
		handler: handler,
		logger:  logger,
	}

	broker := fmt.Sprintf("tcp://%s:%s", host, port)

	opts := pahomqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(30 * time.Second).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetKeepAlive(60 * time.Second).
		SetCleanSession(true).
		SetOnConnectHandler(c.onConnect).
		SetConnectionLostHandler(c.onConnectionLost).
		SetReconnectingHandler(c.onReconnecting)

	if username != "" {
		opts.SetUsername(username)
	}
	if password != "" {
		opts.SetPassword(password)
	}

	c.client = pahomqtt.NewClient(opts)
	return c
}

// Connect starts the MQTT connection. Blocks until connected or timeout.
func (c *Client) Connect() error {
	c.logger.Info("connecting to MQTT broker")
	token := c.client.Connect()
	if !token.WaitTimeout(30 * time.Second) {
		return fmt.Errorf("mqtt connect timeout")
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	return nil
}

// Disconnect cleanly disconnects from the broker.
func (c *Client) Disconnect() {
	c.logger.Info("disconnecting from MQTT broker")
	c.client.Disconnect(1000)
}

func (c *Client) onConnect(client pahomqtt.Client) {
	c.logger.Info("connected to MQTT broker, subscribing to topics")
	for _, carID := range c.carIDs {
		topics := TopicsForCar(carID)
		for _, topic := range topics {
			token := client.Subscribe(topic, 0, c.onMessage)
			token.Wait()
			if err := token.Error(); err != nil {
				c.logger.Error("failed to subscribe", "topic", topic, "error", err)
			}
		}
		c.logger.Info("subscribed to car topics", "car_id", carID, "topic_count", len(topics))
	}
}

func (c *Client) onConnectionLost(_ pahomqtt.Client, err error) {
	c.logger.Warn("MQTT connection lost", "error", err)
}

func (c *Client) onReconnecting(_ pahomqtt.Client, _ *pahomqtt.ClientOptions) {
	c.logger.Info("reconnecting to MQTT broker")
}

func (c *Client) onMessage(_ pahomqtt.Client, msg pahomqtt.Message) {
	carID, field, ok := ParseTopic(msg.Topic())
	if !ok {
		c.logger.Warn("ignoring unrecognized topic", "topic", msg.Topic())
		return
	}
	value := string(msg.Payload())
	c.logger.Debug("mqtt message", "car_id", carID, "field", string(field), "value", value)
	c.handler(carID, field, value)
}
