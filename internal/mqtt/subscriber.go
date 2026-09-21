//receive messages

package mqtt

import paho "github.com/eclipse/paho.mqtt.golang"

type Subscriber struct {
	client *Client
}

func NewSubscriber(client *Client) *Subscriber {
	return &Subscriber{
		client: client,
	}
}

func (s *Subscriber) Subscribe(
	topic string,
	handler paho.MessageHandler,
) error {
	return s.client.Subscribe(topic, handler)
}
