// create the mqtt connection
package mqtt

import (
	"fmt"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

type Config struct {
	BrokerURL string
	ClientID  string
}

type Client struct {
	client paho.Client
}

func NewClient(cfg Config) (*Client, error) {
	opts := paho.NewClientOptions()

	opts.AddBroker(cfg.BrokerURL)
	opts.SetClientID(cfg.ClientID)

	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(2 * time.Second)

	client := paho.NewClient(opts)

	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		return nil, fmt.Errorf("mqtt connect: %w", token.Error())
	}

	return &Client{
		client: client,
	}, nil
}

func (c *Client) Publish(
	topic string,
	payload []byte,
) error {
	token := c.client.Publish(topic, 0, false, payload)

	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("mqtt publish: %w", token.Error())
	}

	return nil
}

func (c *Client) Subscribe(
	topic string,
	handler paho.MessageHandler,
) error {
	token := c.client.Subscribe(topic, 0, handler)

	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("mqtt subscribe: %w", token.Error())
	}

	return nil
}

func (c *Client) Close() {
	if c.client.IsConnected() {
		c.client.Disconnect(1000)
	}
}
