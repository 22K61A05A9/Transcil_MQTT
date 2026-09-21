// publish messages
package mqtt

type Publisher struct {
	client *Client
}

func NewPublisher(client *Client) *Publisher {
	return &Publisher{
		client: client,
	}
}

func (p *Publisher) Publish(
	topic string,
	payload []byte,
) error {
	return p.client.Publish(topic, payload)
}
