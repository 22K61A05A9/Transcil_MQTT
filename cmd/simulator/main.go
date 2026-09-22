package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"gopkg.in/yaml.v3"

	"swap-station-simulator/internal/heartbeat"
	"swap-station-simulator/internal/logger"
	"swap-station-simulator/internal/mqtt"
	"swap-station-simulator/internal/protocol"
	"swap-station-simulator/internal/station"
)

type Config struct {
	MQTT struct {
		Broker string `yaml:"broker"`
	} `yaml:"mqtt"`

	Station struct {
		Code      string `yaml:"code"`
		MachineID string `yaml:"machine_id"`
	} `yaml:"station"`

	Heartbeat struct {
		Interval string `yaml:"interval"`
	} `yaml:"heartbeat"`

	Logging struct {
		SimulatorFile string `yaml:"simulator_file"`
		ServerFile    string `yaml:"server_file"`
	} `yaml:"logging"`
}

type Simulator struct {
	station      *station.Station
	stateMachine *station.StateMachine
	publisher    *mqtt.Publisher
	logger       *logger.Logger
	// Context used by batteries that leave the station.
	// When the simulator shuts down, their drain goroutines
	// also stop.
	ctx context.Context
}

func main() {
	// ------------------------------------------------------------
	// Load configuration
	// ------------------------------------------------------------

	config, err := loadConfig("config/config.yaml")
	if err != nil {
		fmt.Printf("failed to load config: %v\n", err)
		os.Exit(1)
	}

	interval, err := time.ParseDuration(config.Heartbeat.Interval)
	if err != nil {
		fmt.Printf(
			"invalid heartbeat interval %q: %v\n",
			config.Heartbeat.Interval,
			err,
		)
		os.Exit(1)
	}

	// ------------------------------------------------------------
	// Create shutdown context
	// ------------------------------------------------------------

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()
	simLogger, err := logger.New(config.Logging.SimulatorFile)
	if err != nil {
		fmt.Printf("failed to create simulator logger: %v\n", err)
		os.Exit(1)
	}
	defer simLogger.Close()
	// ------------------------------------------------------------
	// Create simulated station
	// ------------------------------------------------------------

	// Five cabinet slots:
	//
	// Slot 1 -> BAT001 -> 95%
	// Slot 2 -> BAT002 -> 93%
	// Slot 3 -> BAT003 -> 91%
	// Slot 4 -> BAT004 -> 60% and charging
	// Slot 5 -> EMPTY
	//
	// InitializeBatteries() creates this state.

	simulatedStation := station.NewStation(
		config.Station.Code,
		config.Station.MachineID,
		5,
	)

	if err := simulatedStation.InitializeBatteries(); err != nil {
		fmt.Printf(
			"failed to initialize station batteries: %v\n",
			err,
		)
		os.Exit(1)
	}

	stateMachine := station.NewStateMachine(simulatedStation)

	// ------------------------------------------------------------
	// Connect to MQTT
	// ------------------------------------------------------------

	mqttClient, err := mqtt.NewClient(mqtt.Config{
		BrokerURL: config.MQTT.Broker,
		ClientID:  "simulator-" + config.Station.Code,
	})
	if err != nil {
		fmt.Printf("failed to connect to MQTT: %v\n", err)
		os.Exit(1)
	}

	defer mqttClient.Close()

	publisher := mqtt.NewPublisher(mqttClient)
	subscriber := mqtt.NewSubscriber(mqttClient)

	simulator := &Simulator{
		station:      simulatedStation,
		stateMachine: stateMachine,
		publisher:    publisher,
		logger:       simLogger,
		ctx:          ctx,
	}

	// ------------------------------------------------------------
	// Subscribe to station command topic
	// ------------------------------------------------------------

	commandTopic := mqtt.BMSAckTopic(config.Station.Code)

	if err := subscriber.Subscribe(
		commandTopic,
		simulator.handleMessage,
	); err != nil {
		fmt.Printf(
			"failed to subscribe to %s: %v\n",
			commandTopic,
			err,
		)
		os.Exit(1)
	}

	// ------------------------------------------------------------
	// Create heartbeat
	// ------------------------------------------------------------

	hb := heartbeat.New(
		simulatedStation,
		publisher,
		heartbeat.Config{
			Interval: interval,
		},
	)

	// ------------------------------------------------------------
	// Start battery charging simulation
	// ------------------------------------------------------------

	// Batteries inside the BSS with ChargeStatus == "1"
	// increase their SOC by 1% every minute.

	go simulatedStation.StartBatteryCharging(ctx)

	// ------------------------------------------------------------
	// Startup information
	// ------------------------------------------------------------

	fmt.Println("========================================")
	fmt.Println(" Battery Swap Station Simulator")
	fmt.Println("========================================")
	fmt.Printf("Station  : %s\n", config.Station.Code)
	fmt.Printf("Machine  : %s\n", config.Station.MachineID)
	fmt.Printf("MQTT     : %s\n", config.MQTT.Broker)
	fmt.Printf("Heartbeat: %s\n", interval)
	fmt.Printf("Command  : %s\n", commandTopic)
	fmt.Println()

	fmt.Println("Initial station state:")
	printStationState(simulatedStation)

	fmt.Println()
	fmt.Println("Battery charging simulation: +1% SOC every minute")
	fmt.Println("Removed battery simulation: -1% SOC every minute")
	fmt.Println("Swap session: one rider at a time")
	fmt.Println()
	fmt.Println("Simulator started.")
	simLogger.Info(
		"simulator started: station=%s machine=%s broker=%s heartbeat=%s",
		config.Station.Code,
		config.Station.MachineID,
		config.MQTT.Broker,
		interval,
	)
	fmt.Println("Press Ctrl+C to stop.")
	fmt.Println()

	// ------------------------------------------------------------
	// Run heartbeat
	// ------------------------------------------------------------

	if err := hb.Run(ctx); err != nil {
		if ctx.Err() != nil {
			fmt.Println("Simulator stopped.")
			return
		}

		fmt.Printf("heartbeat stopped: %v\n", err)
		os.Exit(1)
	}
}

// ============================================================
// MQTT MESSAGE HANDLER
// ============================================================

func (s *Simulator) handleMessage(
	_ paho.Client,
	message paho.Message,
) {
	raw := string(message.Payload())

	fmt.Println()
	fmt.Println("========== MQTT COMMAND ==========")
	fmt.Printf("Topic   : %s\n", message.Topic())
	fmt.Printf("Payload : %s\n", raw)

	s.logger.Info(
		"MQTT message received: topic=%s payload=%s",
		message.Topic(),
		raw,
	)

	messageType, parsed, err := protocol.Parse(raw)
	if err != nil {
		fmt.Printf("Parse error: %v\n", err)

		s.logger.Error(
			"CALON message parse failed: error=%v payload=%s",
			err,
			raw,
		)

		fmt.Println("==================================")
		return
	}

	// We only process CALON$11.
	//
	// CALON$02 is the response published by the simulator.
	if messageType != protocol.MessageCalon11 {
		fmt.Printf(
			"Ignoring message type: %s\n",
			messageType,
		)

		s.logger.Info(
			"MQTT message ignored: message_type=%s",
			messageType,
		)

		fmt.Println("==================================")
		return
	}

	command, ok := parsed.(protocol.Calon11)
	if !ok {
		fmt.Println("Parse error: invalid CALON$11 structure")

		s.logger.Error(
			"invalid CALON$11 structure",
		)

		fmt.Println("==================================")
		return
	}

	requestedSlot := "automatic"
	if command.SlotID > 0 {
		requestedSlot = strconv.Itoa(command.SlotID)
	}

	fmt.Printf(
		"CALON$11 received: rider=%s requested_slot=%s swap_state=%d open=%d\n",
		command.RiderID,
		requestedSlot,
		command.SwapState,
		command.OpenBatterySlot,
	)

	s.logger.Info(
		"CALON$11 received: rider=%s requested_slot=%s swap_state=%d open=%d",
		command.RiderID,
		requestedSlot,
		command.SwapState,
		command.OpenBatterySlot,
	)

	if err := s.processCommand(command); err != nil {
		fmt.Printf("Command rejected: %v\n", err)

		s.logger.Error(
			"swap command rejected: rider=%s requested_slot=%d reason=%v",
			command.RiderID,
			command.SlotID,
			err,
		)

		// Send an immediate application-level failure response
		// so the server does not wait for the 10-second timeout.
		if responseErr := s.publishFailureResponse(command, err); responseErr != nil {
			fmt.Printf("Failed to publish rejection response: %v\n", responseErr)

			s.logger.Error(
				"failure CALON$02 publish failed: rider=%s error=%v",
				command.RiderID,
				responseErr,
			)
		}

		fmt.Println("==================================")
		return
	}

	s.logger.Info(
		"swap command processed successfully: rider=%s requested_slot=%d",
		command.RiderID,
		command.SlotID,
	)

	fmt.Println("Command processed successfully.")
	fmt.Println("==================================")
}

// ============================================================
// PUBLISH FAILURE CALON$02
// ============================================================
//
// This is an application-level simulator response used when
// a swap cannot be completed, for example:
//   - no empty slot is available for the returned battery
//   - no battery with SOC >= 90% is available
//
// The confirmed CALON$02 protocol does not define a dedicated
// failure-reason field. The reason is therefore logged locally.
// The response uses SlotID=0 and zeroed result flags.
func (s *Simulator) publishFailureResponse(
	command protocol.Calon11,
	reason error,
) error {
	snapshot := s.station.Snapshot()

	responseCommand := command
	responseCommand.SlotID = 0

	// buildCalon02() intentionally returns the protocol failure
	// structure when SlotID is 0.
	response := s.buildCalon02(responseCommand)

	response.RiderID = command.RiderID
	response.SlotID = 0
	response.BatterySerial = "0"
	response.BluetoothID = "0"
	response.HeartbeatAck = 0
	response.SwapState = 0
	response.EmptySlotOpenStatus = 0
	response.Buzzer = 0

	// Keep current cabinet information in the failure response.
	response.Cabinet.StationID = snapshot.Code
	response.Cabinet.MachineID = snapshot.MachineID
	response.Cabinet.FireSense = snapshot.FireSensor
	response.Cabinet.Power = snapshot.Power
	response.Cabinet.Temp = snapshot.Temperature
	response.Cabinet.WaterLevel = snapshot.WaterLevel
	response.Cabinet.PhaseReadings = snapshot.PhaseReadings
	response.Cabinet.GSMSignal = snapshot.SignalStrength
	response.Cabinet.SlotCount = len(snapshot.Slots)

	payload, err := protocol.SerializeCalon02(response)
	if err != nil {
		return fmt.Errorf("serialize failure CALON$02: %w", err)
	}

	wrappedPayload := "ACK," + payload
	topic := mqtt.BMSAckTopic(s.station.Code)

	if err := s.publisher.Publish(topic, []byte(wrappedPayload)); err != nil {
		return fmt.Errorf("publish failure CALON$02: %w", err)
	}

	fmt.Println()
	fmt.Println("========== SWAP REJECTED RESPONSE ==========")
	fmt.Printf("Rider  : %s\n", command.RiderID)
	fmt.Printf("Reason : %v\n", reason)
	fmt.Println("CALON$02 failure response published.")
	fmt.Println("============================================")

	s.logger.Info(
		"failure CALON$02 published: rider=%s reason=%v topic=%s",
		command.RiderID,
		reason,
		topic,
	)

	return nil
}

// ============================================================
// CALON$11 PROCESSING
// ============================================================
//
// Complete simulated rider swap:
//
//  1. Lock station for rider
//  2. Find empty slot
//  3. Open empty slot
//  4. Receive rider's low battery
//  5. Seat and lock returned battery
//  6. Returned battery starts charging
//  7. Find station battery >= 90%
//  8. Open charged battery slot
//  9. Remove charged battery
//
// 10. Charged battery leaves station
// 11. Removed battery starts draining -1%/minute
// 12. Publish CALON$02
// 13. Release station
func (s *Simulator) processCommand(
	command protocol.Calon11,
) error {

	// ------------------------------------------------------------
	// STEP 0: Validate command
	// ------------------------------------------------------------

	if command.RiderID == "" {
		return fmt.Errorf("rider_id is required")
	}

	// SlotID == 0 means automatic charged-battery selection.
	// A positive SlotID is still supported for protocol/testing
	// purposes, but the normal server flow sends 0.
	if command.SlotID < 0 {
		return fmt.Errorf(
			"invalid slot_id=%d",
			command.SlotID,
		)
	}

	if command.OpenBatterySlot != 1 {
		return fmt.Errorf(
			"unsupported command: open_battery_slot=%d",
			command.OpenBatterySlot,
		)
	}

	// For the currently confirmed simulator flow we expect
	// swap_state=1 (swap started).
	if command.SwapState != 1 {
		return fmt.Errorf(
			"unsupported swap_state=%d; expected 1",
			command.SwapState,
		)
	}

	// ------------------------------------------------------------
	// STEP 1: Lock station for this rider
	// ------------------------------------------------------------

	if err := s.station.BeginSwap(command.RiderID); err != nil {
		return err
	}

	s.logger.Info(
		"swap started: rider=%s station=%s",
		command.RiderID,
		s.station.Code,
	)

	// Always release the station when this swap finishes.
	defer func() {
		s.station.EndSwap()

		s.logger.Info(
			"station released: rider=%s",
			command.RiderID,
		)
	}()

	fmt.Println()
	fmt.Println("========== SWAP START ==========")
	fmt.Printf("Rider   : %s\n", command.RiderID)
	fmt.Println("Station : LOCKED for this rider")
	fmt.Println("================================")

	// ------------------------------------------------------------
	// STEP 2: PRE-CHECK SWAP REQUIREMENTS
	// ------------------------------------------------------------
	//
	// IMPORTANT:
	// We must check BOTH requirements before opening the
	// return slot or accepting the rider's battery.
	//
	// Requirement 1:
	//   There must be an empty slot for the returned battery.
	//
	// Requirement 2:
	//   There must be a station battery with SOC >= 90%
	//   available to give to the rider.
	//
	// This prevents the station from accepting a returned
	// battery and then discovering that it cannot provide
	// a charged battery.
	// ------------------------------------------------------------

	emptySlot := s.station.FindEmptySlot()

	if emptySlot == nil {
		s.logger.Error(
			"swap pre-check failed: rider=%s reason=no empty slot available",
			command.RiderID,
		)

		return fmt.Errorf(
			"no empty slot available for returned battery",
		)
	}

	// Select the charged battery BEFORE accepting the returned
	// battery. There is no concurrency with another swap because
	// BeginSwap() has already locked the station for this rider.
	chargedSlot := s.findRequestedOrAvailableChargedSlot(
		command.SlotID,
	)

	if chargedSlot == nil {
		s.logger.Error(
			"swap pre-check failed: rider=%s reason=no battery with SOC >= 90%%",
			command.RiderID,
		)

		return fmt.Errorf(
			"no battery with SOC >= 90%% is available",
		)
	}

	emptySlotNumber := emptySlot.Number
	chargedSlotNumber := chargedSlot.Number
	chargedBatteryID := chargedSlot.Battery.ID
	chargedBatterySOC := chargedSlot.Battery.ChargePercentage

	s.logger.Info(
		"swap pre-check passed: rider=%s return_slot=%d charged_slot=%d battery=%s soc=%.2f",
		command.RiderID,
		emptySlotNumber,
		chargedSlotNumber,
		chargedBatteryID,
		chargedBatterySOC,
	)

	fmt.Printf(
		"Swap pre-check passed: return slot=%d | charged slot=%d | battery=%s | SOC=%.2f%%\n",
		emptySlotNumber,
		chargedSlotNumber,
		chargedBatteryID,
		chargedBatterySOC,
	)

	// ------------------------------------------------------------
	// STEP 3: Open empty slot
	// ------------------------------------------------------------

	if err := s.stateMachine.OpenSlot(emptySlotNumber); err != nil {
		s.logger.Error(
			"failed to open return slot: rider=%s slot=%d error=%v",
			command.RiderID,
			emptySlotNumber,
			err,
		)

		return fmt.Errorf(
			"open empty slot %d: %w",
			emptySlotNumber,
			err,
		)
	}

	s.printSlotState(
		emptySlotNumber,
		"DOOR_OPEN_FOR_RETURN",
	)

	s.logger.Info(
		"return slot opened: rider=%s slot=%d",
		command.RiderID,
		emptySlotNumber,
	)

	time.Sleep(1 * time.Second)

	// ------------------------------------------------------------
	// STEP 4: Rider places low battery into empty slot
	// ------------------------------------------------------------

	returnedBattery := createSimulatedBattery(command)

	s.logger.Info(
		"returned battery created: rider=%s battery=%s bluetooth=%s soc=%.2f",
		command.RiderID,
		returnedBattery.ID,
		returnedBattery.BluetoothID,
		returnedBattery.ChargePercentage,
	)

	fmt.Println()
	fmt.Printf(
		"Rider returned battery: %s | SOC=%.2f%%\n",
		returnedBattery.ID,
		returnedBattery.ChargePercentage,
	)

	if err := s.stateMachine.DetectBattery(
		emptySlotNumber,
		returnedBattery,
	); err != nil {
		s.logger.Error(
			"failed to detect returned battery: rider=%s slot=%d battery=%s error=%v",
			command.RiderID,
			emptySlotNumber,
			returnedBattery.ID,
			err,
		)

		return fmt.Errorf(
			"detect returned battery in slot %d: %w",
			emptySlotNumber,
			err,
		)
	}

	s.printSlotState(
		emptySlotNumber,
		"BATTERY_DETECTED",
	)

	s.logger.Info(
		"returned battery detected: rider=%s slot=%d battery=%s",
		command.RiderID,
		emptySlotNumber,
		returnedBattery.ID,
	)

	time.Sleep(1 * time.Second)

	// ------------------------------------------------------------
	// STEP 5: Seat returned battery
	// ------------------------------------------------------------

	if err := s.stateMachine.SeatBattery(emptySlotNumber); err != nil {
		s.logger.Error(
			"failed to seat returned battery: rider=%s slot=%d error=%v",
			command.RiderID,
			emptySlotNumber,
			err,
		)

		return fmt.Errorf(
			"seat returned battery in slot %d: %w",
			emptySlotNumber,
			err,
		)
	}

	s.printSlotState(
		emptySlotNumber,
		"BATTERY_SEATED",
	)

	s.logger.Info(
		"returned battery seated: rider=%s slot=%d battery=%s",
		command.RiderID,
		emptySlotNumber,
		returnedBattery.ID,
	)

	time.Sleep(1 * time.Second)

	// ------------------------------------------------------------
	// STEP 6: Lock returned battery
	// ------------------------------------------------------------

	if err := s.stateMachine.LockSlot(emptySlotNumber); err != nil {
		s.logger.Error(
			"failed to lock returned battery: rider=%s slot=%d error=%v",
			command.RiderID,
			emptySlotNumber,
			err,
		)

		return fmt.Errorf(
			"lock returned battery in slot %d: %w",
			emptySlotNumber,
			err,
		)
	}

	s.printSlotState(
		emptySlotNumber,
		"RETURNED_BATTERY_LOCKED",
	)

	s.logger.Info(
		"returned battery locked and charging: rider=%s slot=%d battery=%s soc=%.2f",
		command.RiderID,
		emptySlotNumber,
		returnedBattery.ID,
		returnedBattery.ChargePercentage,
	)

	fmt.Printf(
		"Returned battery %s is now charging inside Slot %d\n",
		returnedBattery.ID,
		emptySlotNumber,
	)

	// ------------------------------------------------------------
	// STEP 7: Use the charged battery selected during pre-check
	// ------------------------------------------------------------

	// Re-read the slot so we use the current station state.
	// The station is still locked for this rider, so no other
	// swap can modify this slot.
	chargedSlot = s.station.GetSlot(chargedSlotNumber)

	if chargedSlot == nil ||
		chargedSlot.Battery == nil ||
		chargedSlot.State != station.SlotLocked ||
		!chargedSlot.Occupied ||
		chargedSlot.Battery.ChargePercentage < 90 {

		return fmt.Errorf(
			"selected charged battery is no longer available: slot=%d",
			chargedSlotNumber,
		)
	}

	chargedBatteryID = chargedSlot.Battery.ID
	chargedBatterySOC = chargedSlot.Battery.ChargePercentage

	s.logger.Info(
		"charged battery selected: rider=%s slot=%d battery=%s soc=%.2f",
		command.RiderID,
		chargedSlotNumber,
		chargedBatteryID,
		chargedBatterySOC,
	)

	fmt.Println()
	fmt.Printf(
		"Charged battery selected: Slot %d -> %s -> SOC=%.2f%%\n",
		chargedSlotNumber,
		chargedBatteryID,
		chargedBatterySOC,
	)

	// ------------------------------------------------------------
	// STEP 8: Open charged battery slot
	// ------------------------------------------------------------

	if err := s.stateMachine.OpenSlot(chargedSlotNumber); err != nil {
		s.logger.Error(
			"failed to open charged battery slot: rider=%s slot=%d battery=%s error=%v",
			command.RiderID,
			chargedSlotNumber,
			chargedBatteryID,
			err,
		)

		return fmt.Errorf(
			"open charged battery slot %d: %w",
			chargedSlotNumber,
			err,
		)
	}

	s.printSlotState(
		chargedSlotNumber,
		"CHARGED_BATTERY_DOOR_OPEN",
	)

	s.logger.Info(
		"charged battery slot opened: rider=%s slot=%d battery=%s",
		command.RiderID,
		chargedSlotNumber,
		chargedBatteryID,
	)

	time.Sleep(1 * time.Second)

	// ------------------------------------------------------------
	// STEP 9: Remove charged battery
	// ------------------------------------------------------------

	removedBattery, err := s.stateMachine.RemoveBattery(
		chargedSlotNumber,
	)
	if err != nil {
		s.logger.Error(
			"failed to remove charged battery: rider=%s slot=%d error=%v",
			command.RiderID,
			chargedSlotNumber,
			err,
		)

		return fmt.Errorf(
			"remove charged battery from slot %d: %w",
			chargedSlotNumber,
			err,
		)
	}

	fmt.Println()
	fmt.Println("========== BATTERY SWAP ==========")
	fmt.Printf("Battery given to rider : %s\n", removedBattery.ID)
	fmt.Printf("Bluetooth ID           : %s\n", removedBattery.BluetoothID)
	fmt.Printf("Starting SOC           : %.2f%%\n", removedBattery.ChargePercentage)
	fmt.Printf("Removed from Slot      : %d\n", chargedSlotNumber)
	fmt.Println("==================================")

	s.logger.Info(
		"charged battery removed: rider=%s slot=%d battery=%s bluetooth=%s soc=%.2f",
		command.RiderID,
		chargedSlotNumber,
		removedBattery.ID,
		removedBattery.BluetoothID,
		removedBattery.ChargePercentage,
	)

	// ------------------------------------------------------------
	// STEP 10: Start battery drain
	// ------------------------------------------------------------

	go s.station.DrainBattery(
		s.ctx,
		removedBattery,
	)

	s.logger.Info(
		"battery drain started: rider=%s battery=%s soc=%.2f rate=-1%%/minute",
		command.RiderID,
		removedBattery.ID,
		removedBattery.ChargePercentage,
	)

	fmt.Printf(
		"Battery %s is now outside BSS.\n",
		removedBattery.ID,
	)
	fmt.Println("Battery drain simulation: -1% SOC every minute")

	// ------------------------------------------------------------
	// STEP 11: Build CALON$02
	// ------------------------------------------------------------

	responseCommand := command
	responseCommand.SlotID = chargedSlotNumber

	response := s.buildCalon02(responseCommand)

	payload, err := protocol.SerializeCalon02(response)
	if err != nil {
		s.logger.Error(
			"failed to serialize CALON$02: rider=%s error=%v",
			command.RiderID,
			err,
		)

		return fmt.Errorf(
			"serialize CALON$02: %w",
			err,
		)
	}

	// Real station response format:
	//
	// ACK,CALON$02,...
	wrappedPayload := "ACK," + payload

	topic := mqtt.BMSAckTopic(
		s.station.Code,
	)

	// ------------------------------------------------------------
	// STEP 11A: Publish CALON$02
	// ------------------------------------------------------------

	if err := s.publisher.Publish(
		topic,
		[]byte(wrappedPayload),
	); err != nil {
		s.logger.Error(
			"failed to publish CALON$02: rider=%s topic=%s error=%v",
			command.RiderID,
			topic,
			err,
		)

		return fmt.Errorf(
			"publish CALON$02: %w",
			err,
		)
	}

	s.logger.Info(
		"CALON$02 published: rider=%s slot=%d battery_given=%s topic=%s",
		command.RiderID,
		chargedSlotNumber,
		removedBattery.ID,
		topic,
	)

	// ------------------------------------------------------------
	// STEP 12: Swap complete
	// ------------------------------------------------------------

	fmt.Println()
	fmt.Println("========== SWAP COMPLETE ==========")
	fmt.Printf("Rider          : %s\n", command.RiderID)
	fmt.Printf("Returned       : %s\n", returnedBattery.ID)
	fmt.Printf("Returned slot  : %d\n", emptySlotNumber)
	fmt.Printf("Given to rider : %s\n", removedBattery.ID)
	fmt.Printf("Charged slot   : %d\n", chargedSlotNumber)
	fmt.Printf(
		"Given battery SOC: %.2f%%\n",
		removedBattery.ChargePercentage,
	)
	fmt.Println("Station        : READY")
	fmt.Println("===================================")

	fmt.Printf(
		"Published CALON$02: %s\n",
		wrappedPayload,
	)

	s.logger.Info(
		"swap completed: rider=%s returned=%s returned_slot=%d given=%s charged_slot=%d soc=%.2f",
		command.RiderID,
		returnedBattery.ID,
		emptySlotNumber,
		removedBattery.ID,
		chargedSlotNumber,
		removedBattery.ChargePercentage,
	)

	return nil
}

// ============================================================
// FIND CHARGED BATTERY
// ============================================================
//
// If CALON$11 specifies a slot >= 1, use that slot only when
// it contains a locked battery with SOC >= 90%.
//
// If SlotID == 0, the server is asking the simulator to
// automatically select an available charged battery.
//
// SlotID == 0 is an application-level simulator convention.
// It is not being introduced as a new CALON protocol value.
func (s *Simulator) findRequestedOrAvailableChargedSlot(
	requestedSlotNumber int,
) *station.Slot {

	if requestedSlotNumber >= 1 {
		requestedSlot := s.station.GetSlot(
			requestedSlotNumber,
		)

		if requestedSlot != nil &&
			requestedSlot.Battery != nil &&
			requestedSlot.State == station.SlotLocked &&
			requestedSlot.Occupied &&
			requestedSlot.Battery.ChargePercentage >= 90 {

			return requestedSlot
		}
	}

	return s.station.FindChargedBatterySlot()
}

// ============================================================
// CREATE SIMULATED BATTERY
// ============================================================
//
// This represents the low battery that the rider brings
// back to the station.
//
// Default SOC = 80%
// ChargeStatus = 1
//
// Therefore, once it is locked into the BSS:
//
// 80 -> 81 -> 82 -> ...
//
// +1% every minute.
func createSimulatedBattery(
	command protocol.Calon11,
) *station.Battery {

	batteryID := command.BatterySerial

	if batteryID == "" || batteryID == "0" {
		// The server no longer asks the rider to select a slot,
		// so SlotID is 0 for automatic selection.
		// Use the rider ID to create a readable simulated
		// battery identity instead of generating SIMBAT000.
		batteryID = fmt.Sprintf(
			"SIMBAT-%s",
			command.RiderID,
		)
	}

	bluetoothID := command.BluetoothID

	if bluetoothID == "" || bluetoothID == "0" {
		bluetoothID = fmt.Sprintf(
			"SIMBT-%s",
			command.RiderID,
		)
	}

	return &station.Battery{
		ID:          batteryID,
		BluetoothID: bluetoothID,

		TotalVoltage:     520.00,
		TotalCurrent:     10.00,
		ChargePercentage: 80.00,
		HealthPercentage: 95.00,
		UsableCapacity:   40.00,

		CellTemperature: 28.00,

		ChargeSwitch:    true,
		DischargeSwitch: true,

		HighestCellVoltage:     4.20,
		LowestCellVoltage:      4.18,
		HighestCellTemperature: 29.00,
		LowestCellTemperature:  27.00,

		Alarm: "0",

		// Returned battery starts charging once it is
		// inserted and locked into the station.
		ChargeStatus: "1",

		ChargingVoltage: 520.00,
		ChargingCurrent: 10.00,
		ChargingAlarm:   "0",
	}
}

// ============================================================
// PRINT STATION STATE
// ============================================================

func printStationState(
	s *station.Station,
) {
	snapshot := s.Snapshot()

	for _, slot := range snapshot.Slots {
		if slot.Battery == nil {
			fmt.Printf(
				"  Slot %d -> EMPTY\n",
				slot.Number,
			)
			continue
		}

		fmt.Printf(
			"  Slot %d -> %s | SOC=%.2f%% | charger=%s | state=%s\n",
			slot.Number,
			slot.Battery.ID,
			slot.Battery.ChargePercentage,
			slot.Battery.ChargeStatus,
			slot.State,
		)
	}
}

// ============================================================
// PRINT SLOT STATE
// ============================================================

func (s *Simulator) printSlotState(
	slotNumber int,
	state string,
) {
	slot := s.station.GetSlot(slotNumber)

	if slot == nil {
		return
	}

	fmt.Printf(
		"Slot %d state: %s | door=%d | occupied=%d | battery=%s\n",
		slotNumber,
		state,
		boolToInt(slot.DoorOpen),
		boolToInt(slot.Occupied),
		batteryID(slot.Battery),
	)
}

func batteryID(
	battery *station.Battery,
) string {
	if battery == nil {
		return "NONE"
	}

	return battery.ID
}

// ============================================================
// BUILD CALON$02
// ============================================================

func (s *Simulator) buildCalon02(
	command protocol.Calon11,
) protocol.Calon02 {

	snapshot := s.station.Snapshot()

	// Safety check.
	if command.SlotID < 1 ||
		command.SlotID > len(snapshot.Slots) {
		return protocol.Calon02{
			Cabinet: protocol.CabinetStatus{
				Date:          time.Now().Format("02012006"),
				Time:          time.Now().Format("150405"),
				StationID:     snapshot.Code,
				MachineID:     snapshot.MachineID,
				FireSense:     snapshot.FireSensor,
				Power:         snapshot.Power,
				Temp:          snapshot.Temperature,
				WaterLevel:    snapshot.WaterLevel,
				PhaseReadings: snapshot.PhaseReadings,
				GSMSignal:     snapshot.SignalStrength,
				SlotCount:     len(snapshot.Slots),
			},

			RiderID:             command.RiderID,
			SlotID:              0,
			BatterySerial:       "0",
			BluetoothID:         "0",
			HeartbeatAck:        0,
			SwapState:           0,
			EmptySlotOpenStatus: 0,
			Buzzer:              0,

			Slot: protocol.SlotStatus{
				SlotID:            "0",
				DoorStatus:        "0",
				CabinetTemp:       "25",
				CabinetOnline:     "1",
				SlotOccupancy:     "0",
				DoorMalfunction:   "0",
				BatterySerial:     "0",
				BluetoothID:       "0",
				TotalVoltage:      "0",
				TotalCurrent:      "0",
				SOC:               "0",
				SOH:               "0",
				RemainingCapacity: "0",
				CellTemp:          "0",
				CFETStatus:        "0",
				DFETStatus:        "0",
				MaxCellVoltage:    "0",
				MinCellVoltage:    "0",
				MaxCellTemp:       "0",
				MinCellTemp:       "0",
				BMSAlarms:         "0",
				ChargerStatus:     "0",
				ChargingVoltage:   "0",
				ChargingCurrent:   "0",
				ChargerAlarms:     "0",
			},
		}
	}

	slot := snapshot.Slots[command.SlotID-1]

	// Use the actual battery currently in the selected slot.
	//
	// After the charged battery has been removed, this will
	// normally be "0", because the slot is now empty.
	batterySerial := "0"
	bluetoothID := "0"

	if slot.Battery != nil {
		batterySerial = slot.Battery.ID
		bluetoothID = slot.Battery.BluetoothID
	}

	return protocol.Calon02{
		Cabinet: protocol.CabinetStatus{
			Date:          time.Now().Format("02012006"),
			Time:          time.Now().Format("150405"),
			StationID:     snapshot.Code,
			MachineID:     snapshot.MachineID,
			FireSense:     snapshot.FireSensor,
			Power:         snapshot.Power,
			Temp:          snapshot.Temperature,
			WaterLevel:    snapshot.WaterLevel,
			PhaseReadings: snapshot.PhaseReadings,
			GSMSignal:     snapshot.SignalStrength,
			SlotCount:     len(snapshot.Slots),
		},

		RiderID:       command.RiderID,
		SlotID:        command.SlotID,
		BatterySerial: batterySerial,
		BluetoothID:   bluetoothID,

		// Confirmed successful command behavior.
		HeartbeatAck: 1,
		SwapState:    command.SwapState,

		EmptySlotOpenStatus: boolToInt(slot.DoorOpen),

		Buzzer: command.Buzzer,

		Slot: buildResponseSlot(slot),
	}
}

// ============================================================
// BUILD SLOT STATUS
// ============================================================

func buildResponseSlot(
	slot station.Slot,
) protocol.SlotStatus {

	status := protocol.SlotStatus{
		SlotID:          strconv.Itoa(slot.Number),
		DoorStatus:      boolToIntString(slot.DoorOpen),
		CabinetTemp:     "25",
		CabinetOnline:   boolToIntString(slot.Online),
		SlotOccupancy:   boolToIntString(slot.Occupied),
		DoorMalfunction: boolToIntString(slot.DoorFault),
	}

	// ------------------------------------------------------------
	// Empty slot
	// ------------------------------------------------------------

	if slot.Battery == nil {
		status.BatterySerial = "0"
		status.BluetoothID = "0"

		status.TotalVoltage = "0"
		status.TotalCurrent = "0"

		status.SOC = "0"
		status.SOH = "0"

		status.RemainingCapacity = "0"
		status.CellTemp = "0"

		status.CFETStatus = "0"
		status.DFETStatus = "0"

		status.MaxCellVoltage = "0"
		status.MinCellVoltage = "0"

		status.MaxCellTemp = "0"
		status.MinCellTemp = "0"

		status.BMSAlarms = "0"
		status.ChargerStatus = "0"
		status.ChargingVoltage = "0"
		status.ChargingCurrent = "0"
		status.ChargerAlarms = "0"

		return status
	}

	// ------------------------------------------------------------
	// Battery present
	// ------------------------------------------------------------

	battery := slot.Battery

	status.BatterySerial = battery.ID
	status.BluetoothID = battery.BluetoothID

	status.TotalVoltage = formatFloat(
		battery.TotalVoltage,
	)

	status.TotalCurrent = formatFloat(
		battery.TotalCurrent,
	)

	status.SOC = formatFloat(
		battery.ChargePercentage,
	)

	status.SOH = formatFloat(
		battery.HealthPercentage,
	)

	status.RemainingCapacity = formatFloat(
		battery.UsableCapacity,
	)

	status.CellTemp = formatFloat(
		battery.CellTemperature,
	)

	status.CFETStatus = boolToIntString(
		battery.ChargeSwitch,
	)

	status.DFETStatus = boolToIntString(
		battery.DischargeSwitch,
	)

	status.MaxCellVoltage = formatFloat(
		battery.HighestCellVoltage,
	)

	status.MinCellVoltage = formatFloat(
		battery.LowestCellVoltage,
	)

	status.MaxCellTemp = formatFloat(
		battery.HighestCellTemperature,
	)

	status.MinCellTemp = formatFloat(
		battery.LowestCellTemperature,
	)

	status.BMSAlarms = battery.Alarm
	status.ChargerStatus = battery.ChargeStatus

	status.ChargingVoltage = formatFloat(
		battery.ChargingVoltage,
	)

	status.ChargingCurrent = formatFloat(
		battery.ChargingCurrent,
	)

	status.ChargerAlarms = battery.ChargingAlarm

	return status
}

// ============================================================
// HELPERS
// ============================================================

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func boolToIntString(value bool) string {
	if value {
		return "1"
	}

	return "0"
}

func formatFloat(value float64) string {
	return fmt.Sprintf("%.2f", value)
}

// ============================================================
// CONFIG
// ============================================================

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf(
			"read config file: %w",
			err,
		)
	}

	var config Config

	if err := yaml.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf(
			"parse config file: %w",
			err,
		)
	}

	if config.MQTT.Broker == "" {
		return Config{}, fmt.Errorf(
			"mqtt broker is required",
		)
	}

	if config.Station.Code == "" {
		return Config{}, fmt.Errorf(
			"station code is required",
		)
	}

	if config.Station.MachineID == "" {
		return Config{}, fmt.Errorf(
			"station machine_id is required",
		)
	}

	if config.Heartbeat.Interval == "" {
		return Config{}, fmt.Errorf(
			"heartbeat interval is required",
		)
	}

	return config, nil
}
