package heartbeat

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"swap-station-simulator/internal/mqtt"
	"swap-station-simulator/internal/protocol"
	"swap-station-simulator/internal/station"
)

type Config struct {
	Interval time.Duration
}

type Heartbeat struct {
	station   *station.Station
	publisher *mqtt.Publisher
	config    Config
}

func New(
	station *station.Station,
	publisher *mqtt.Publisher,
	config Config,
) *Heartbeat {
	return &Heartbeat{
		station:   station,
		publisher: publisher,
		config:    config,
	}
}

// Run starts the heartbeat loop.
//
// A CALON$01 heartbeat is published periodically to:
//
// telemetry/bms/raw
func (h *Heartbeat) Run(ctx context.Context) error {
	if h.config.Interval <= 0 {
		return fmt.Errorf(
			"heartbeat interval must be greater than zero",
		)
	}

	ticker := time.NewTicker(h.config.Interval)
	defer ticker.Stop()

	// Send one heartbeat immediately when the simulator starts.
	if err := h.publish(); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			if err := h.publish(); err != nil {
				return err
			}
		}
	}
}

func (h *Heartbeat) publish() error {
	message := h.buildMessage()

	payload, err := protocol.SerializeCalon01(message)
	if err != nil {
		return fmt.Errorf(
			"serialize CALON$01: %w",
			err,
		)
	}

	if err := h.publisher.Publish(
		mqtt.TelemetryBMSRaw,
		[]byte(payload),
	); err != nil {
		return fmt.Errorf(
			"publish CALON$01: %w",
			err,
		)
	}

	return nil
}

func (h *Heartbeat) buildMessage() protocol.Calon01 {
	snapshot := h.station.Snapshot()

	cabinet := protocol.CabinetStatus{
		Date:       time.Now().Format("02012006"),
		Time:       time.Now().Format("150405"),
		StationID:  snapshot.Code,
		MachineID:  snapshot.MachineID,
		FireSense:  snapshot.FireSensor,
		Power:      snapshot.Power,
		Temp:       snapshot.Temperature,
		WaterLevel: snapshot.WaterLevel,

		PhaseReadings: snapshot.PhaseReadings,

		GSMSignal: snapshot.SignalStrength,
		SlotCount: len(snapshot.Slots),
	}

	slots := make(
		[]protocol.SlotStatus,
		0,
		len(snapshot.Slots),
	)

	for _, slot := range snapshot.Slots {
		slots = append(
			slots,
			buildSlotStatus(slot),
		)
	}

	return protocol.Calon01{
		Cabinet: cabinet,
		Slots:   slots,
	}
}

func buildSlotStatus(slot station.Slot) protocol.SlotStatus {
	status := protocol.SlotStatus{
		SlotID: strconv.Itoa(slot.Number),

		DoorStatus:      boolToString(slot.DoorOpen),
		CabinetTemp:     "25",
		CabinetOnline:   boolToString(slot.Online),
		SlotOccupancy:   boolToString(slot.Occupied),
		DoorMalfunction: boolToString(slot.DoorFault),
	}

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

	battery := slot.Battery

	status.BatterySerial = battery.ID
	status.BluetoothID = battery.BluetoothID

	status.TotalVoltage = formatFloat(battery.TotalVoltage)
	status.TotalCurrent = formatFloat(battery.TotalCurrent)

	status.SOC = formatFloat(battery.ChargePercentage)
	status.SOH = formatFloat(battery.HealthPercentage)

	status.RemainingCapacity = formatFloat(battery.UsableCapacity)
	status.CellTemp = formatFloat(battery.CellTemperature)

	status.CFETStatus = boolToString(battery.ChargeSwitch)
	status.DFETStatus = boolToString(battery.DischargeSwitch)

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

	// The protocol reference confirms that the production firmware
	// currently duplicates total voltage/current into these fields.
	status.ChargingVoltage = formatFloat(
		battery.ChargingVoltage,
	)

	status.ChargingCurrent = formatFloat(
		battery.ChargingCurrent,
	)

	status.ChargerAlarms = battery.ChargingAlarm

	return status
}

func boolToString(value bool) string {
	if value {
		return "1"
	}

	return "0"
}

func formatFloat(value float64) string {
	return fmt.Sprintf("%.2f", value)
}
