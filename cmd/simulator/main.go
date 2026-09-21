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
	// Create simulated station
	// ------------------------------------------------------------

	simulatedStation := station.NewStation(
		config.Station.Code,
		config.Station.MachineID,
		3,
	)

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
	// Graceful shutdown
	// ------------------------------------------------------------

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

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
	fmt.Println("Simulator started.")
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

	messageType, parsed, err := protocol.Parse(raw)
	if err != nil {
		fmt.Printf("Parse error: %v\n", err)
		fmt.Println("==================================")
		return
	}

	// We only process CALON$11.
	// CALON$02 is our response and can come back because
	// we subscribe to the same MQTT topic.

	if messageType != protocol.MessageCalon11 {
		fmt.Printf(
			"Ignoring message type: %s\n",
			messageType,
		)
		fmt.Println("==================================")
		return
	}

	command, ok := parsed.(protocol.Calon11)
	if !ok {
		fmt.Println("Parse error: invalid CALON$11 structure")
		fmt.Println("==================================")
		return
	}

	fmt.Printf(
		"CALON$11 received: rider=%s slot=%d swap_state=%d open=%d\n",
		command.RiderID,
		command.SlotID,
		command.SwapState,
		command.OpenBatterySlot,
	)

	if err := s.processCommand(command); err != nil {
		fmt.Printf("Command error: %v\n", err)
		fmt.Println("==================================")
		return
	}

	fmt.Println("Command processed successfully.")
	fmt.Println("==================================")
}

// ============================================================
// CALON$11 PROCESSING
// ============================================================

func (s *Simulator) processCommand(
	command protocol.Calon11,
) error {
	// ------------------------------------------------------------
	// Validate slot
	// ------------------------------------------------------------

	slot := s.station.GetSlot(command.SlotID)

	if slot == nil {
		return fmt.Errorf(
			"slot %d does not exist",
			command.SlotID,
		)
	}

	// ------------------------------------------------------------
	// Initial supported return command
	// ------------------------------------------------------------

	if command.OpenBatterySlot != 1 {
		return fmt.Errorf(
			"unsupported command: open_battery_slot=%d",
			command.OpenBatterySlot,
		)
	}

	// ------------------------------------------------------------
	// STEP 1: Open slot
	// ------------------------------------------------------------

	if err := s.stateMachine.OpenSlot(
		command.SlotID,
	); err != nil {
		return fmt.Errorf(
			"open slot %d: %w",
			command.SlotID,
			err,
		)
	}

	s.printSlotState(
		command.SlotID,
		"DOOR_OPEN",
	)

	time.Sleep(1 * time.Second)

	// ------------------------------------------------------------
	// STEP 2: Detect battery
	// ------------------------------------------------------------

	battery := createSimulatedBattery(command)

	if err := s.stateMachine.DetectBattery(
		command.SlotID,
		battery,
	); err != nil {
		return fmt.Errorf(
			"detect battery in slot %d: %w",
			command.SlotID,
			err,
		)
	}

	s.printSlotState(
		command.SlotID,
		"BATTERY_DETECTED",
	)

	time.Sleep(1 * time.Second)

	// ------------------------------------------------------------
	// STEP 3: Seat battery
	// ------------------------------------------------------------

	if err := s.stateMachine.SeatBattery(
		command.SlotID,
	); err != nil {
		return fmt.Errorf(
			"seat battery in slot %d: %w",
			command.SlotID,
			err,
		)
	}

	s.printSlotState(
		command.SlotID,
		"BATTERY_SEATED",
	)

	time.Sleep(1 * time.Second)

	// ------------------------------------------------------------
	// STEP 4: Lock slot
	// ------------------------------------------------------------

	if err := s.stateMachine.LockSlot(
		command.SlotID,
	); err != nil {
		return fmt.Errorf(
			"lock slot %d: %w",
			command.SlotID,
			err,
		)
	}

	s.printSlotState(
		command.SlotID,
		"LOCKED",
	)

	// ------------------------------------------------------------
	// STEP 5: Build CALON$02
	// ------------------------------------------------------------

	response := s.buildCalon02(command)

	payload, err := protocol.SerializeCalon02(response)
	if err != nil {
		return fmt.Errorf(
			"serialize CALON$02: %w",
			err,
		)
	}

	// Real response format:
	//
	// ACK,CALON$02,...
	//

	wrappedPayload := "ACK," + payload

	topic := mqtt.BMSAckTopic(
		s.station.Code,
	)

	if err := s.publisher.Publish(
		topic,
		[]byte(wrappedPayload),
	); err != nil {
		return fmt.Errorf(
			"publish CALON$02: %w",
			err,
		)
	}

	fmt.Println()
	fmt.Println("========== SWAP COMPLETE ==========")
	fmt.Printf("Station : %s\n", s.station.Code)
	fmt.Printf("Slot    : %d\n", command.SlotID)
	fmt.Printf("Battery : %s\n", battery.ID)
	fmt.Println("State   : LOCKED")
	fmt.Println("===================================")

	fmt.Printf(
		"Published CALON$02: %s\n",
		wrappedPayload,
	)

	return nil
}

// ============================================================
// CREATE SIMULATED BATTERY
// ============================================================

func createSimulatedBattery(
	command protocol.Calon11,
) *station.Battery {
	batteryID := command.BatterySerial

	if batteryID == "" || batteryID == "0" {
		batteryID = fmt.Sprintf(
			"SIMBAT%03d",
			command.SlotID,
		)
	}

	bluetoothID := command.BluetoothID

	if bluetoothID == "" || bluetoothID == "0" {
		bluetoothID = fmt.Sprintf(
			"SIMBT%03d",
			command.SlotID,
		)
	}

	return &station.Battery{
		ID:          batteryID,
		BluetoothID: bluetoothID,

		TotalVoltage: 520.00,
		TotalCurrent: 10.00,

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

		ChargeStatus: "0",

		ChargingVoltage: 520.00,
		ChargingCurrent: 10.00,
		ChargingAlarm:   "0",
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

	slot := snapshot.Slots[command.SlotID-1]

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
		BatterySerial: command.BatterySerial,
		BluetoothID:   command.BluetoothID,

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
