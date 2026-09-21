package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

// SerializeCalon01 converts a CALON$01 structure into the
// exact CALON wire format.
//
// Format:
//
// CALON$01,<16 cabinet fields>,<25 slot fields>...,#
func SerializeCalon01(message Calon01) (string, error) {
	if message.Cabinet.SlotCount != len(message.Slots) {
		return "", fmt.Errorf(
			"CALON$01: cabinet slot_count=%d but got %d slots",
			message.Cabinet.SlotCount,
			len(message.Slots),
		)
	}

	fields := []string{
		string(MessageCalon01),
	}

	fields = append(fields, serializeCabinet(message.Cabinet)...)

	for _, slot := range message.Slots {
		fields = append(fields, serializeSlot(slot)...)
	}

	return strings.Join(fields, ",") + ",#", nil
}

// SerializeCalon02 converts a CALON$02 structure into the
// exact CALON wire format.
//
// Format:
//
// CALON$02,<16 cabinet fields>,<8 response fields>,<25 slot fields>,#
func SerializeCalon02(message Calon02) (string, error) {
	fields := []string{
		string(MessageCalon02),
	}

	fields = append(fields, serializeCabinet(message.Cabinet)...)

	fields = append(fields,
		message.RiderID,
		strconv.Itoa(message.SlotID),
		message.BatterySerial,
		message.BluetoothID,
		strconv.Itoa(message.HeartbeatAck),
		strconv.Itoa(message.SwapState),
		strconv.Itoa(message.EmptySlotOpenStatus),
		strconv.Itoa(message.Buzzer),
	)

	fields = append(fields, serializeSlot(message.Slot)...)

	return strings.Join(fields, ",") + ",#", nil
}

// SerializeCalon11 converts a CALON$11 structure into the
// exact CALON wire format.
//
// Format:
//
// CALON$11,<date>,<time>,<rider_id>,<slot_id>,
// <battery_serial>,<bluetooth_id>,<heartbeat_ack>,
// <swap_state>,<open_battery_slot>,<buzzer>,#
func SerializeCalon11(message Calon11) string {
	fields := []string{
		string(MessageCalon11),

		message.Date,
		message.Time,

		message.RiderID,

		strconv.Itoa(message.SlotID),

		message.BatterySerial,
		message.BluetoothID,

		strconv.Itoa(message.HeartbeatAck),
		strconv.Itoa(message.SwapState),
		strconv.Itoa(message.OpenBatterySlot),
		strconv.Itoa(message.Buzzer),
	}

	return strings.Join(fields, ",") + ",#"
}

// serializeCabinet converts the common 16-field cabinet structure
// into protocol field order.
func serializeCabinet(cabinet CabinetStatus) []string {
	return []string{
		cabinet.Date,
		cabinet.Time,

		cabinet.StationID,
		cabinet.MachineID,

		cabinet.FireSense,
		cabinet.Power,
		cabinet.Temp,
		cabinet.WaterLevel,

		cabinet.PhaseReadings[0],
		cabinet.PhaseReadings[1],
		cabinet.PhaseReadings[2],
		cabinet.PhaseReadings[3],
		cabinet.PhaseReadings[4],
		cabinet.PhaseReadings[5],

		cabinet.GSMSignal,
		strconv.Itoa(cabinet.SlotCount),
	}
}

// serializeSlot converts the 25-field battery slot structure
// into protocol field order.
func serializeSlot(slot SlotStatus) []string {
	return []string{
		slot.SlotID,

		slot.DoorStatus,
		slot.CabinetTemp,
		slot.CabinetOnline,
		slot.SlotOccupancy,
		slot.DoorMalfunction,

		slot.BatterySerial,
		slot.BluetoothID,

		slot.TotalVoltage,
		slot.TotalCurrent,

		slot.SOC,
		slot.SOH,

		slot.RemainingCapacity,
		slot.CellTemp,

		slot.CFETStatus,
		slot.DFETStatus,

		slot.MaxCellVoltage,
		slot.MinCellVoltage,

		slot.MaxCellTemp,
		slot.MinCellTemp,

		slot.BMSAlarms,

		slot.ChargerStatus,

		slot.ChargingVoltage,
		slot.ChargingCurrent,
		slot.ChargerAlarms,
	}
}
