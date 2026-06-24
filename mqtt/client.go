package mqtt

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

// MessageHandler is called when a parsed MQTT message is received.
type MessageHandler func(carID string, field TopicField, value string)

// Client wraps the Paho MQTT client with auto-reconnect and topic subscriptions.
type Client struct {
	client    pahomqtt.Client
	carIDs    []string
	carSet    map[string]bool // fast lookup for car ID filtering
	namespace string          // MQTT topic namespace (e.g. "/my_namespace")
	handler     MessageHandler
	onConnectCb func()
	logger      *slog.Logger
}

// SetOnConnect sets a callback run on each (re)connect, before subscribing.
func (c *Client) SetOnConnect(fn func()) { c.onConnectCb = fn }

// NewClient creates a new MQTT client. Call Connect() to start.
func NewClient(host, port, username, password, clientID string, useTLS bool, namespace string, carIDs []string, handler MessageHandler, logger *slog.Logger) *Client {
	carSet := make(map[string]bool, len(carIDs))
	for _, id := range carIDs {
		carSet[id] = true
	}

	// Format namespace prefix: "ns" → "/ns", "" → ""
	nsPart := ""
	if namespace != "" {
		nsPart = "/" + namespace
	}

	c := &Client{
		carIDs:    carIDs,
		carSet:    carSet,
		namespace: nsPart,
		handler:   handler,
		logger:    logger,
	}

	scheme := "tcp"
	if useTLS {
		scheme = "tls"
	}
	broker := fmt.Sprintf("%s://%s:%s", scheme, host, port)

	opts := pahomqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(30 * time.Second).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetKeepAlive(60 * time.Second).
		SetCleanSession(true).
		SetOrderMatters(false).
		SetOnConnectHandler(c.onConnect).
		SetConnectionLostHandler(c.onConnectionLost).
		SetReconnectingHandler(c.onReconnecting)

	if useTLS {
		opts.SetTLSConfig(&tls.Config{})
	}

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

	// Before subscribing — retained messages land right after.
	if c.onConnectCb != nil {
		c.onConnectCb()
	}

	// Single wildcard subscription — matches all cars and all fields.
	// Retained messages for every topic are delivered immediately.
	// Explicit callback (not nil) ensures messages are routed correctly,
	// matching the pattern used by TeslaMateAPI.
	topic := fmt.Sprintf("teslamate%s/cars/#", c.namespace)
	token := client.Subscribe(topic, 0, c.onMessage)
	token.Wait()
	if err := token.Error(); err != nil {
		c.logger.Error("failed to subscribe", "topic", topic, "error", err)
		return
	}
	c.logger.Info("subscribed to wildcard topic", "topic", topic)
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
		return // not a recognized topic format, silently ignore
	}

	// Only process cars we're monitoring
	if !c.carSet[carID] {
		return
	}

	// Only process fields we care about
	if !knownFields[field] {
		return
	}

	value := string(msg.Payload())
	c.logger.Debug("mqtt message", "car_id", carID, "field", string(field), "value", value)
	c.handler(carID, field, value)
}
