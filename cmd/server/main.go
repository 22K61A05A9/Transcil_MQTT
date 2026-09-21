package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"swap-station-simulator/internal/mqtt"
	"swap-station-simulator/internal/protocol"
)

type Server struct {
	mqttClient *mqtt.Client
	publisher  *mqtt.Publisher
	subscriber *mqtt.Subscriber

	stationCode string

	responseCh chan protocol.Calon02
}

func main() {
	fmt.Println("========================================")
	fmt.Println(" Battery Swap Server")
	fmt.Println("========================================")

	stationCode := "SSLOCALDEV001"
	brokerURL := "tcp://localhost:1883"

	fmt.Printf("Station : %s\n", stationCode)
	fmt.Printf("MQTT    : %s\n", brokerURL)

	client, err := mqtt.NewClient(mqtt.Config{
		BrokerURL: brokerURL,
		ClientID:  "swap-server",
	})
	if err != nil {
		fmt.Printf("MQTT connection failed: %v\n", err)
		return
	}
	defer client.Close()

	publisher := mqtt.NewPublisher(client)
	subscriber := mqtt.NewSubscriber(client)

	server := &Server{
		mqttClient:  client,
		publisher:   publisher,
		subscriber:  subscriber,
		stationCode: stationCode,

		responseCh: make(chan protocol.Calon02, 1),
	}

	ackTopic := mqtt.BMSAckTopic(stationCode)

	err = subscriber.Subscribe(
		ackTopic,
		server.handleMessage,
	)
	if err != nil {
		fmt.Printf("MQTT subscribe failed: %v\n", err)
		return
	}

	fmt.Printf("Listening : %s\n", ackTopic)
	fmt.Println()
	fmt.Println("Server started.")
	fmt.Println()

	ctx := context.Background()

	for {
		if err := server.runCommand(ctx); err != nil {
			fmt.Printf("Command failed: %v\n", err)
		}

		fmt.Println()
		fmt.Println("----------------------------------------")
		fmt.Println("Ready for another swap command.")
		fmt.Println("----------------------------------------")
		fmt.Println()
	}
}

func (s *Server) runCommand(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("========== NEW SWAP ==========")

	riderID, err := readInput(
		reader,
		"Rider ID: ",
	)
	if err != nil {
		return err
	}

	slotInput, err := readInput(
		reader,
		"Slot ID: ",
	)
	if err != nil {
		return err
	}

	slotID, err := strconv.Atoi(slotInput)
	if err != nil {
		return fmt.Errorf(
			"invalid slot ID %q",
			slotInput,
		)
	}

	if slotID < 1 {
		return fmt.Errorf(
			"slot ID must be greater than zero",
		)
	}

	command := protocol.Calon11{
		Date: time.Now().Format("02012006"),
		Time: time.Now().Format("150405"),

		RiderID: riderID,

		SlotID: slotID,

		BatterySerial: "0",
		BluetoothID:   "0",

		HeartbeatAck:    1,
		SwapState:       1,
		OpenBatterySlot: 1,
		Buzzer:          1,
	}

	payload := protocol.SerializeCalon11(command)

	topic := mqtt.BMSAckTopic(s.stationCode)

	fmt.Println()
	fmt.Println("========== SENDING COMMAND ==========")
	fmt.Printf("Topic   : %s\n", topic)
	fmt.Printf("Payload : %s\n", payload)

	if err := s.publisher.Publish(
		topic,
		[]byte(payload),
	); err != nil {
		return fmt.Errorf(
			"publish CALON$11: %w",
			err,
		)
	}

	fmt.Println("Command published successfully.")
	fmt.Println("Waiting for CALON$02 response...")

	// Wait until the simulator sends CALON$02.
	select {
	case response := <-s.responseCh:
		s.printResponse(response)

		return nil

	case <-time.After(10 * time.Second):
		return fmt.Errorf(
			"timeout waiting for CALON$02 response",
		)

	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) handleMessage(
	_ paho.Client,
	message paho.Message,
) {
	payload := strings.TrimSpace(
		string(message.Payload()),
	)

	fmt.Println()
	fmt.Println("========== MQTT MESSAGE ==========")
	fmt.Printf("Topic   : %s\n", message.Topic())
	fmt.Printf("Payload : %s\n", payload)

	// The server subscribes to the same topic on which it
	// publishes CALON$11. Therefore the server can see its
	// own outgoing CALON$11.
	if strings.Contains(payload, "CALON$11") {
		fmt.Println("Ignoring own CALON$11 command.")
		fmt.Println("===================================")
		return
	}

	if !strings.Contains(payload, "CALON$02") {
		fmt.Printf(
			"Ignoring message type: %s\n",
			detectMessageType(payload),
		)
		fmt.Println("===================================")
		return
	}

	response, err := protocol.ParseCalon02(payload)
	if err != nil {
		fmt.Printf(
			"Failed to parse CALON$02: %v\n",
			err,
		)
		fmt.Println("===================================")
		return
	}

	// Send the parsed response to runCommand().
	s.responseCh <- response
}

func (s *Server) printResponse(
	response protocol.Calon02,
) {
	fmt.Println()
	fmt.Println("========== SWAP RESPONSE ==========")

	fmt.Printf(
		"Station       : %s\n",
		response.Cabinet.StationID,
	)

	fmt.Printf(
		"Rider         : %s\n",
		response.RiderID,
	)

	fmt.Printf(
		"Slot          : %d\n",
		response.SlotID,
	)

	fmt.Printf(
		"Battery       : %s\n",
		response.BatterySerial,
	)

	fmt.Printf(
		"Bluetooth     : %s\n",
		response.BluetoothID,
	)

	fmt.Printf(
		"Heartbeat Ack : %d\n",
		response.HeartbeatAck,
	)

	fmt.Printf(
		"Swap State    : %d\n",
		response.SwapState,
	)

	fmt.Printf(
		"Empty Slot    : %d\n",
		response.EmptySlotOpenStatus,
	)

	fmt.Printf(
		"Buzzer        : %d\n",
		response.Buzzer,
	)

	fmt.Println()
	fmt.Println("Battery telemetry:")

	fmt.Printf(
		"  Voltage     : %s\n",
		response.Slot.TotalVoltage,
	)

	fmt.Printf(
		"  Current     : %s\n",
		response.Slot.TotalCurrent,
	)

	fmt.Printf(
		"  SOC         : %s\n",
		response.Slot.SOC,
	)

	fmt.Printf(
		"  SOH         : %s\n",
		response.Slot.SOH,
	)

	fmt.Printf(
		"  Temperature : %s\n",
		response.Slot.CellTemp,
	)

	fmt.Println("===================================")
}

func readInput(
	reader *bufio.Reader,
	prompt string,
) (string, error) {
	fmt.Print(prompt)

	value, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	value = strings.TrimSpace(value)

	if value == "" {
		return "", fmt.Errorf(
			"input cannot be empty",
		)
	}

	return value, nil
}

func detectMessageType(payload string) string {
	switch {
	case strings.Contains(payload, "CALON$01"):
		return "CALON$01"

	case strings.Contains(payload, "CALON$02"):
		return "CALON$02"

	case strings.Contains(payload, "CALON$11"):
		return "CALON$11"

	default:
		return "UNKNOWN"
	}
}