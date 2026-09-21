package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

type MessageType string

const (
	MessageCalon01 MessageType = "CALON$01"
	MessageCalon02 MessageType = "CALON$02"
	MessageCalon11 MessageType = "CALON$11"
)

// Parse detects the CALON message type and parses the complete message.
func Parse(raw string) (MessageType, any, error) {
	messageType, err := DetectMessageType(raw)
	if err != nil {
		return "", nil, err
	}

	switch messageType {
	case MessageCalon01:
		message, err := ParseCalon01(raw)
		if err != nil {
			return "", nil, err
		}

		return messageType, message, nil

	case MessageCalon02:
		message, err := ParseCalon02(raw)
		if err != nil {
			return "", nil, err
		}

		return messageType, message, nil

	case MessageCalon11:
		message, err := ParseCalon11(raw)
		if err != nil {
			return "", nil, err
		}

		return messageType, message, nil

	default:
		return "", nil, fmt.Errorf(
			"unsupported CALON message type: %s",
			messageType,
		)
	}
}

// DetectMessageType identifies the CALON frame contained in the raw payload.
func DetectMessageType(raw string) (MessageType, error) {
	raw = normalizeRaw(raw)

	switch {
	case strings.Contains(raw, string(MessageCalon01)):
		return MessageCalon01, nil

	case strings.Contains(raw, string(MessageCalon02)):
		return MessageCalon02, nil

	case strings.Contains(raw, string(MessageCalon11)):
		return MessageCalon11, nil

	default:
		return "", fmt.Errorf("unknown CALON message type")
	}
}

// ParseCalon01 parses a complete CALON$01 heartbeat.
//
// CALON$01:
//
// CALON$01
// + 16 cabinet fields
// + 25 fields per slot
func ParseCalon01(raw string) (Calon01, error) {
	fields, err := splitFrame(raw)
	if err != nil {
		return Calon01{}, err
	}

	// 1 message type + 16 cabinet fields.
	if len(fields) < 17 {
		return Calon01{}, fmt.Errorf(
			"CALON$01: expected at least 17 fields, got %d",
			len(fields),
		)
	}

	if fields[0] != string(MessageCalon01) {
		return Calon01{}, fmt.Errorf(
			"CALON$01: invalid message type %q",
			fields[0],
		)
	}

	cabinet, err := parseCabinet(fields[1:17])
	if err != nil {
		return Calon01{}, fmt.Errorf(
			"CALON$01 cabinet: %w",
			err,
		)
	}

	expectedFields := 17 + cabinet.SlotCount*25

	if len(fields) != expectedFields {
		return Calon01{}, fmt.Errorf(
			"CALON$01: expected %d fields for %d slots, got %d",
			expectedFields,
			cabinet.SlotCount,
			len(fields),
		)
	}

	slots := make([]SlotStatus, cabinet.SlotCount)

	offset := 17

	for i := 0; i < cabinet.SlotCount; i++ {
		slot, err := parseSlot(fields[offset : offset+25])
		if err != nil {
			return Calon01{}, fmt.Errorf(
				"CALON$01 slot %d: %w",
				i+1,
				err,
			)
		}

		slots[i] = slot
		offset += 25
	}

	return Calon01{
		Cabinet: cabinet,
		Slots:   slots,
	}, nil
}

// ParseCalon02 parses a complete CALON$02 response.
//
// The real MQTT payload may be:
//
// ACK,CALON$02,...
//
// or:
//
// CALON$02,...
func ParseCalon02(raw string) (Calon02, error) {
	fields, err := splitFrame(raw)
	if err != nil {
		return Calon02{}, err
	}

	// The real protocol uses:
	//
	// 1  = message type
	// 16 = cabinet fields
	// 8  = response fields
	// 25 = slot fields
	//
	// Total = 50 fields.
	//
	// If ACK, is present, there will initially be 51 fields.
	if len(fields) > 0 && fields[0] == "ACK" {
		fields = fields[1:]
	}

	const expectedFields = 50

	if len(fields) != expectedFields {
		return Calon02{}, fmt.Errorf(
			"CALON$02: expected %d fields after optional ACK wrapper, got %d",
			expectedFields,
			len(fields),
		)
	}

	if fields[0] != string(MessageCalon02) {
		return Calon02{}, fmt.Errorf(
			"CALON$02: invalid message type %q",
			fields[0],
		)
	}

	// ------------------------------------------------------------
	// Cabinet
	// ------------------------------------------------------------

	cabinet, err := parseCabinet(fields[1:17])
	if err != nil {
		return Calon02{}, fmt.Errorf(
			"CALON$02 cabinet: %w",
			err,
		)
	}

	// ------------------------------------------------------------
	// Response fields
	// ------------------------------------------------------------
	//
	// Index:
	//
	// 17 = rider_id
	// 18 = slot_id
	// 19 = battery_serial
	// 20 = bluetooth_id
	// 21 = heartbeat_ack
	// 22 = swap_state
	// 23 = empty_slot_open_status
	// 24 = buzzer
	//
	// 25 = first slot field
	// ------------------------------------------------------------

	slotID, err := strconv.Atoi(fields[18])
	if err != nil {
		return Calon02{}, fmt.Errorf(
			"CALON$02 slot_id %q: %w",
			fields[18],
			err,
		)
	}

	heartbeatAck, err := parseIntField(
		"heartbeat_ack",
		fields[21],
	)
	if err != nil {
		return Calon02{}, err
	}

	swapState, err := parseIntField(
		"swap_state",
		fields[22],
	)
	if err != nil {
		return Calon02{}, err
	}

	emptySlotOpenStatus, err := parseIntField(
		"empty_slot_open_status",
		fields[23],
	)
	if err != nil {
		return Calon02{}, err
	}

	buzzer, err := parseIntField(
		"buzzer",
		fields[24],
	)
	if err != nil {
		return Calon02{}, err
	}

	// Exactly one slot block exists in CALON$02.
	//
	// fields[25:50] = 25 slot fields.
	slot, err := parseSlot(fields[25:50])
	if err != nil {
		return Calon02{}, fmt.Errorf(
			"CALON$02 slot: %w",
			err,
		)
	}

	return Calon02{
		Cabinet: cabinet,

		RiderID: fields[17],

		SlotID: slotID,

		BatterySerial: fields[19],
		BluetoothID:   fields[20],

		HeartbeatAck:        heartbeatAck,
		SwapState:           swapState,
		EmptySlotOpenStatus: emptySlotOpenStatus,
		Buzzer:              buzzer,

		Slot: slot,
	}, nil
}

// ParseCalon11 parses a CALON$11 command.
//
// Required fields:
//
// CALON$11
// date
// time
// rider_id
// slot_id
// battery_serial
// bluetooth_id
// heartbeat_ack
// swap_state
// open_battery_slot
// buzzer
//
// The protocol reference says extra trailing fields are accepted by
// the real station, so this parser requires the first 11 fields only.
func ParseCalon11(raw string) (Calon11, error) {
	fields, err := splitFrame(raw)
	if err != nil {
		return Calon11{}, err
	}

	if len(fields) < 11 {
		return Calon11{}, fmt.Errorf(
			"CALON$11: expected at least 11 fields, got %d",
			len(fields),
		)
	}

	if fields[0] != string(MessageCalon11) {
		return Calon11{}, fmt.Errorf(
			"CALON$11: invalid message type %q",
			fields[0],
		)
	}

	slotID, err := strconv.Atoi(fields[4])
	if err != nil {
		return Calon11{}, fmt.Errorf(
			"CALON$11 slot_id %q: %w",
			fields[4],
			err,
		)
	}

	heartbeatAck, err := parseIntField(
		"heartbeat_ack",
		fields[7],
	)
	if err != nil {
		return Calon11{}, err
	}

	swapState, err := parseIntField(
		"swap_state",
		fields[8],
	)
	if err != nil {
		return Calon11{}, err
	}

	openBatterySlot, err := parseIntField(
		"open_battery_slot",
		fields[9],
	)
	if err != nil {
		return Calon11{}, err
	}

	buzzer, err := parseIntField(
		"buzzer",
		fields[10],
	)
	if err != nil {
		return Calon11{}, err
	}

	return Calon11{
		Date: fields[1],
		Time: fields[2],

		RiderID: fields[3],

		SlotID: slotID,

		BatterySerial: fields[5],
		BluetoothID:   fields[6],

		HeartbeatAck:    heartbeatAck,
		SwapState:       swapState,
		OpenBatterySlot: openBatterySlot,
		Buzzer:          buzzer,
	}, nil
}

// parseCabinet parses the common 16-field cabinet block.
//
// Field order:
//
//  1. date
//  2. time
//  3. station_id
//  4. machine_id
//  5. fire_sense
//  6. power
//  7. temp
//  8. water_level
//  9. phase A voltage
//
// 10. phase A current
// 11. phase B voltage
// 12. phase B current
// 13. phase C voltage
// 14. phase C current
// 15. GSM signal
// 16. slot_count
func parseCabinet(fields []string) (CabinetStatus, error) {
	if len(fields) != 16 {
		return CabinetStatus{}, fmt.Errorf(
			"expected 16 cabinet fields, got %d",
			len(fields),
		)
	}

	slotCount, err := strconv.Atoi(fields[15])
	if err != nil {
		return CabinetStatus{}, fmt.Errorf(
			"slot_count %q: %w",
			fields[15],
			err,
		)
	}

	if slotCount < 0 {
		return CabinetStatus{}, fmt.Errorf(
			"slot_count cannot be negative: %d",
			slotCount,
		)
	}

	return CabinetStatus{
		Date: fields[0],
		Time: fields[1],

		StationID: fields[2],
		MachineID: fields[3],

		FireSense:  fields[4],
		Power:      fields[5],
		Temp:       fields[6],
		WaterLevel: fields[7],

		PhaseReadings: [6]string{
			fields[8],
			fields[9],
			fields[10],
			fields[11],
			fields[12],
			fields[13],
		},

		GSMSignal: fields[14],
		SlotCount: slotCount,
	}, nil
}

// parseSlot parses the 25-field battery slot block.
func parseSlot(fields []string) (SlotStatus, error) {
	if len(fields) != 25 {
		return SlotStatus{}, fmt.Errorf(
			"expected 25 slot fields, got %d",
			len(fields),
		)
	}

	return SlotStatus{
		SlotID: fields[0],

		DoorStatus:      fields[1],
		CabinetTemp:     fields[2],
		CabinetOnline:   fields[3],
		SlotOccupancy:   fields[4],
		DoorMalfunction: fields[5],

		BatterySerial: fields[6],
		BluetoothID:   fields[7],

		TotalVoltage: fields[8],
		TotalCurrent: fields[9],

		SOC: fields[10],
		SOH: fields[11],

		RemainingCapacity: fields[12],
		CellTemp:          fields[13],

		CFETStatus: fields[14],
		DFETStatus: fields[15],

		MaxCellVoltage: fields[16],
		MinCellVoltage: fields[17],

		MaxCellTemp: fields[18],
		MinCellTemp: fields[19],

		BMSAlarms: fields[20],

		ChargerStatus: fields[21],

		ChargingVoltage: fields[22],
		ChargingCurrent: fields[23],
		ChargerAlarms:   fields[24],
	}, nil
}

// parseIntField converts a protocol field into an integer.
func parseIntField(name string, value string) (int, error) {
	result, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf(
			"%s %q: %w",
			name,
			value,
			err,
		)
	}

	return result, nil
}

// normalizeRaw removes harmless transport artifacts.
//
// The protocol may occasionally contain a stray \x1A after the
// terminating # character.
func normalizeRaw(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "\x1A")

	return raw
}

// splitFrame validates the CALON frame terminator and returns
// the comma-separated fields without the final ",#".
func splitFrame(raw string) ([]string, error) {
	raw = normalizeRaw(raw)

	if !strings.HasSuffix(raw, ",#") {
		return nil, fmt.Errorf(
			"invalid CALON frame: missing ',#' terminator",
		)
	}

	body := strings.TrimSuffix(raw, ",#")

	if body == "" {
		return nil, fmt.Errorf(
			"invalid CALON frame: empty payload",
		)
	}

	fields := strings.Split(body, ",")

	return fields, nil
}
