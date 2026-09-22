package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"swap-station-simulator/internal/logger"
	"swap-station-simulator/internal/mqtt"
	"swap-station-simulator/internal/protocol"
)

type Server struct {
	mqttClient *mqtt.Client
	publisher  *mqtt.Publisher
	subscriber *mqtt.Subscriber

	logger *logger.Logger

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

	// --------------------------------------------------
	// Logger
	// --------------------------------------------------

	serverLogger, err := logger.New("logs/server.log")
	if err != nil {
		fmt.Printf("Logger initialization failed: %v\n", err)
		return
	}
	defer serverLogger.Close()

	serverLogger.Info(
		"initializing server: station=%s broker=%s",
		stationCode,
		brokerURL,
	)

	// --------------------------------------------------
	// MQTT
	// --------------------------------------------------

	// Use a unique client ID so multiple server processes
	// can connect to the same MQTT broker simultaneously.
	clientID := fmt.Sprintf(
		"swap-server-%d",
		os.Getpid(),
	)

	serverLogger.Info(
		"connecting to MQTT broker: broker=%s client_id=%s",
		brokerURL,
		clientID,
	)

	client, err := mqtt.NewClient(mqtt.Config{
		BrokerURL: brokerURL,
		ClientID:  clientID,
	})
	if err != nil {
		serverLogger.Error(
			"MQTT connection failed: %v",
			err,
		)

		fmt.Printf(
			"MQTT connection failed: %v\n",
			err,
		)

		return
	}
	defer client.Close()

	serverLogger.Info(
		"MQTT connected successfully: client_id=%s",
		clientID,
	)

	publisher := mqtt.NewPublisher(client)
	subscriber := mqtt.NewSubscriber(client)

	// --------------------------------------------------
	// Server
	// --------------------------------------------------

	server := &Server{
		mqttClient:  client,
		publisher:   publisher,
		subscriber:  subscriber,
		logger:      serverLogger,
		stationCode: stationCode,

		responseCh: make(chan protocol.Calon02, 1),
	}

	// --------------------------------------------------
	// MQTT Subscription
	// --------------------------------------------------

	ackTopic := mqtt.BMSAckTopic(stationCode)

	err = subscriber.Subscribe(
		ackTopic,
		server.handleMessage,
	)
	if err != nil {
		serverLogger.Error(
			"MQTT subscription failed: topic=%s error=%v",
			ackTopic,
			err,
		)

		fmt.Printf(
			"MQTT subscribe failed: %v\n",
			err,
		)

		return
	}

	serverLogger.Info(
		"subscribed to MQTT topic: %s",
		ackTopic,
	)

	fmt.Printf("Listening : %s\n", ackTopic)
	fmt.Println()
	fmt.Println("Server started.")
	fmt.Println()

	serverLogger.Info(
		"server started: station=%s broker=%s topic=%s",
		stationCode,
		brokerURL,
		ackTopic,
	)

	ctx := context.Background()

	// --------------------------------------------------
	// Command Loop
	// --------------------------------------------------

	for {
		if err := server.runCommand(ctx); err != nil {
			serverLogger.Error(
				"command failed: error=%v",
				err,
			)

			fmt.Printf(
				"Command failed: %v\n",
				err,
			)
		}

		fmt.Println()
		fmt.Println("----------------------------------------")
		fmt.Println("Ready for another swap command.")
		fmt.Println("----------------------------------------")
		fmt.Println()
	}
}

// ======================================================
// RUN SWAP COMMAND
// ======================================================

func (s *Server) runCommand(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("========== NEW SWAP ==========")

	// --------------------------------------------------
	// Rider ID
	// --------------------------------------------------

	riderID, err := readInput(
		reader,
		"Rider ID: ",
	)
	if err != nil {
		return err
	}

	s.logger.Info(
		"swap request received from CLI: rider=%s",
		riderID,
	)

	// --------------------------------------------------
	// Build CALON$11
	// --------------------------------------------------

	command := protocol.Calon11{
		Date: time.Now().Format("02012006"),
		Time: time.Now().Format("150405"),

		RiderID: riderID,

		// SlotID = 0 means that the simulator should
		// automatically select an available charged battery.
		//
		// The user does NOT select a slot.
		SlotID: 0,

		BatterySerial: "0",
		BluetoothID:   "0",

		HeartbeatAck:    1,
		SwapState:       1,
		OpenBatterySlot: 1,
		Buzzer:          1,
	}

	payload := protocol.SerializeCalon11(command)

	s.logger.Info(
		"CALON$11 created: rider=%s slot=automatic payload=%s",
		riderID,
		payload,
	)

	topic := mqtt.BMSAckTopic(s.stationCode)

	// --------------------------------------------------
	// Display command
	// --------------------------------------------------

	fmt.Println()
	fmt.Println("========== SENDING COMMAND ==========")
	fmt.Printf("Topic   : %s\n", topic)
	fmt.Printf("Payload : %s\n", payload)

	// --------------------------------------------------
	// Publish CALON$11
	// --------------------------------------------------

	if err := s.publisher.Publish(
		topic,
		[]byte(payload),
	); err != nil {

		s.logger.Error(
			"CALON$11 publish failed: rider=%s error=%v",
			riderID,
			err,
		)

		return fmt.Errorf(
			"publish CALON$11: %w",
			err,
		)
	}

	s.logger.Info(
		"CALON$11 published: rider=%s topic=%s",
		riderID,
		topic,
	)

	fmt.Println("Command published successfully.")
	fmt.Println("Waiting for CALON$02 response...")

	s.logger.Info(
		"waiting for CALON$02: rider=%s timeout=10s",
		riderID,
	)

	// --------------------------------------------------
	// Wait for matching CALON$02
	// --------------------------------------------------

	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()

	for {
		select {

		case response := <-s.responseCh:

			// A server should only consume the response
			// belonging to its current rider.
			if response.RiderID != riderID {
				s.logger.Info(
					"CALON$02 ignored: expected_rider=%s received_rider=%s slot=%d",
					riderID,
					response.RiderID,
					response.SlotID,
				)

				continue
			}

			s.logger.Info(
				"CALON$02 received: rider=%s slot=%d battery=%s heartbeat_ack=%d swap_state=%d",
				response.RiderID,
				response.SlotID,
				response.BatterySerial,
				response.HeartbeatAck,
				response.SwapState,
			)

			// HeartbeatAck=0 or SwapState=0 is treated as an
			// application-level rejected swap response.
			// The confirmed CALON protocol does not define
			// a dedicated operational rejection reason field.
			if response.HeartbeatAck == 0 || response.SwapState == 0 {
				s.printResponse(response)

				fmt.Println()
				fmt.Println("========== SWAP REJECTED ==========")
				fmt.Printf("Rider : %s\n", response.RiderID)
				fmt.Println("Reason: Station could not complete the swap.")
				fmt.Println("===================================")

				s.logger.Info(
					"swap rejected: rider=%s heartbeat_ack=%d swap_state=%d",
					response.RiderID,
					response.HeartbeatAck,
					response.SwapState,
				)

				return nil
			}

			// Successful swap response.
			s.printResponse(response)

			fmt.Println()
			fmt.Println("========== SWAP SUCCESSFUL ==========")
			fmt.Printf("Rider : %s\n", response.RiderID)
			fmt.Println("Station completed the battery swap.")
			fmt.Println("=====================================")

			s.logger.Info(
				"swap successful: rider=%s slot=%d battery=%s",
				response.RiderID,
				response.SlotID,
				response.BatterySerial,
			)

			return nil

		case <-timeout.C:

			s.logger.Error(
				"CALON$02 timeout: rider=%s",
				riderID,
			)

			return fmt.Errorf(
				"timeout waiting for CALON$02 response",
			)

		case <-ctx.Done():

			s.logger.Error(
				"swap command cancelled: rider=%s error=%v",
				riderID,
				ctx.Err(),
			)

			return ctx.Err()
		}
	}
}

// ======================================================
// MQTT MESSAGE HANDLER
// ======================================================

func (s *Server) handleMessage(
	_ paho.Client,
	message paho.Message,
) {
	payload := strings.TrimSpace(
		string(message.Payload()),
	)

	s.logger.Info(
		"MQTT message received: topic=%s payload=%s",
		message.Topic(),
		payload,
	)

	fmt.Println()
	fmt.Println("========== MQTT MESSAGE ==========")
	fmt.Printf("Topic   : %s\n", message.Topic())
	fmt.Printf("Payload : %s\n", payload)

	// --------------------------------------------------
	// CALON$11
	// --------------------------------------------------

	// The server publishes CALON$11 on the same topic
	// that it subscribes to. Therefore the server can
	// receive its own outgoing command.
	if strings.Contains(payload, "CALON$11") {

		fmt.Println("Ignoring own CALON$11 command.")

		s.logger.Info(
			"own CALON$11 ignored: topic=%s",
			message.Topic(),
		)

		fmt.Println("===================================")
		return
	}

	// --------------------------------------------------
	// Unknown message
	// --------------------------------------------------

	if !strings.Contains(payload, "CALON$02") {

		messageType := detectMessageType(payload)

		fmt.Printf(
			"Ignoring message type: %s\n",
			messageType,
		)

		s.logger.Info(
			"MQTT message ignored: message_type=%s topic=%s",
			messageType,
			message.Topic(),
		)

		fmt.Println("===================================")
		return
	}

	// --------------------------------------------------
	// Parse CALON$02
	// --------------------------------------------------

	response, err := protocol.ParseCalon02(payload)
	if err != nil {

		fmt.Printf(
			"Failed to parse CALON$02: %v\n",
			err,
		)

		s.logger.Error(
			"CALON$02 parse failed: error=%v payload=%s",
			err,
			payload,
		)

		fmt.Println("===================================")
		return
	}

	s.logger.Info(
		"CALON$02 parsed successfully: station=%s rider=%s slot=%d battery=%s",
		response.Cabinet.StationID,
		response.RiderID,
		response.SlotID,
		response.BatterySerial,
	)

	// --------------------------------------------------
	// Send response to runCommand()
	// --------------------------------------------------

	s.responseCh <- response
}

// ======================================================
// PRINT CALON$02 RESPONSE
// ======================================================

func (s *Server) printResponse(
	response protocol.Calon02,
) {
	s.logger.Info(
		"swap response: station=%s rider=%s slot=%d heartbeat_ack=%d swap_state=%d empty_slot=%d",
		response.Cabinet.StationID,
		response.RiderID,
		response.SlotID,
		response.HeartbeatAck,
		response.SwapState,
		response.EmptySlotOpenStatus,
	)

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

	// This is the slot actually selected by the station.
	// It is NOT entered by the user.
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

// ======================================================
// READ CLI INPUT
// ======================================================

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

// ======================================================
// DETECT MESSAGE TYPE
// ======================================================

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
