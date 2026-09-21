package station

import (
	"fmt"
	"sync"
)

type Station struct {
	mu sync.RWMutex

	Code      string
	MachineID string

	FireSensor  string
	Power       string
	Temperature string
	WaterLevel  string

	PhaseReadings  [6]string
	SignalStrength string

	Slots []Slot

	// Only one rider can perform a swap at a time.
	swapInProgress bool
	swapRiderID    string
}

func NewStation(
	code string,
	machineID string,
	slotCount int,
) *Station {
	s := &Station{
		Code:      code,
		MachineID: machineID,

		FireSensor:  "0",
		Power:       "1",
		Temperature: "25",
		WaterLevel:  "0",

		PhaseReadings: [6]string{
			"230",
			"10",
			"231",
			"11",
			"229",
			"9",
		},

		SignalStrength: "25",

		Slots: make([]Slot, slotCount),
	}

	for i := range s.Slots {
		s.Slots[i] = Slot{
			Number: i + 1,
			State:  SlotEmpty,
			Online: true,
		}
	}

	return s
}

// InitializeBatteries creates the initial BSS state.
//
// Slot 1 -> 95%
// Slot 2 -> 93%
// Slot 3 -> 91%
// Slot 4 -> 60% and charging
// Slot 5 -> EMPTY
//
// The simulator expects at least five slots.
func (s *Station) InitializeBatteries() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.Slots) < 5 {
		return fmt.Errorf(
			"station requires at least 5 slots, got %d",
			len(s.Slots),
		)
	}

	batteries := []*Battery{
		{
			ID:          "BAT001",
			BluetoothID: "BT001",

			TotalVoltage:     520,
			TotalCurrent:     0,
			ChargePercentage: 95,
			HealthPercentage: 98,
			UsableCapacity:   45,
			CellTemperature:  28,

			ChargeSwitch:    true,
			DischargeSwitch: true,

			HighestCellVoltage:     4.20,
			LowestCellVoltage:      4.18,
			HighestCellTemperature: 29,
			LowestCellTemperature:  27,

			Alarm: "0",

			ChargeStatus: "0",

			ChargingVoltage: 520,
			ChargingCurrent: 0,
			ChargingAlarm:   "0",
		},
		{
			ID:          "BAT002",
			BluetoothID: "BT002",

			TotalVoltage:     520,
			TotalCurrent:     0,
			ChargePercentage: 93,
			HealthPercentage: 97,
			UsableCapacity:   44,
			CellTemperature:  28,

			ChargeSwitch:    true,
			DischargeSwitch: true,

			HighestCellVoltage:     4.20,
			LowestCellVoltage:      4.18,
			HighestCellTemperature: 29,
			LowestCellTemperature:  27,

			Alarm: "0",

			ChargeStatus: "0",

			ChargingVoltage: 520,
			ChargingCurrent: 0,
			ChargingAlarm:   "0",
		},
		{
			ID:          "BAT003",
			BluetoothID: "BT003",

			TotalVoltage:     520,
			TotalCurrent:     0,
			ChargePercentage: 91,
			HealthPercentage: 96,
			UsableCapacity:   43,
			CellTemperature:  28,

			ChargeSwitch:    true,
			DischargeSwitch: true,

			HighestCellVoltage:     4.20,
			LowestCellVoltage:      4.18,
			HighestCellTemperature: 29,
			LowestCellTemperature:  27,

			Alarm: "0",

			ChargeStatus: "0",

			ChargingVoltage: 520,
			ChargingCurrent: 0,
			ChargingAlarm:   "0",
		},
		{
			ID:          "BAT004",
			BluetoothID: "BT004",

			TotalVoltage:     520,
			TotalCurrent:     10,
			ChargePercentage: 60,
			HealthPercentage: 95,
			UsableCapacity:   40,
			CellTemperature:  28,

			ChargeSwitch:    true,
			DischargeSwitch: true,

			HighestCellVoltage:     4.20,
			LowestCellVoltage:      4.18,
			HighestCellTemperature: 29,
			LowestCellTemperature:  27,

			Alarm: "0",

			// Battery is charging.
			ChargeStatus: "1",

			ChargingVoltage: 520,
			ChargingCurrent: 10,
			ChargingAlarm:   "0",
		},
	}

	for i, battery := range batteries {
		s.Slots[i].Battery = battery
		s.Slots[i].Occupied = true
		s.Slots[i].DoorOpen = false
		s.Slots[i].State = SlotLocked
		s.Slots[i].Online = true
		s.Slots[i].DoorFault = false
	}

	// Slot 5 intentionally remains empty.
	s.Slots[4] = Slot{
		Number:    5,
		State:     SlotEmpty,
		Online:    true,
		Occupied:  false,
		DoorOpen:  false,
		DoorFault: false,
		Battery:   nil,
	}

	return nil
}

// GetSlot returns a safe copy of a slot.
//
// The returned slot and battery can be inspected by callers
// without allowing them to modify the station's internal state.
func (s *Station) GetSlot(number int) *Slot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if number < 1 || number > len(s.Slots) {
		return nil
	}

	slot := s.Slots[number-1]

	if slot.Battery != nil {
		battery := *slot.Battery
		slot.Battery = &battery
	}

	return &slot
}

// Snapshot returns a complete copy of the station state.
//
// Heartbeat generation uses this snapshot so that heartbeat
// publishing does not hold the station lock while serializing
// and publishing MQTT data.
func (s *Station) Snapshot() Station {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := Station{
		Code:           s.Code,
		MachineID:      s.MachineID,
		FireSensor:     s.FireSensor,
		Power:          s.Power,
		Temperature:    s.Temperature,
		WaterLevel:     s.WaterLevel,
		PhaseReadings:  s.PhaseReadings,
		SignalStrength: s.SignalStrength,
		Slots:          make([]Slot, len(s.Slots)),

		swapInProgress: s.swapInProgress,
		swapRiderID:    s.swapRiderID,
	}

	for i, slot := range s.Slots {
		snapshot.Slots[i] = slot

		if slot.Battery != nil {
			battery := *slot.Battery
			snapshot.Slots[i].Battery = &battery
		}
	}

	return snapshot
}

// getSlot returns the internal slot.
//
// IMPORTANT:
// The caller must already hold the appropriate station lock.
func (s *Station) getSlot(number int) *Slot {
	if number < 1 || number > len(s.Slots) {
		return nil
	}

	return &s.Slots[number-1]
}

// BeginSwap locks the station for one rider.
//
// Only one rider can use the station at a time.
func (s *Station) BeginSwap(riderID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.swapInProgress {
		return fmt.Errorf(
			"station is busy with rider %s",
			s.swapRiderID,
		)
	}

	s.swapInProgress = true
	s.swapRiderID = riderID

	return nil
}

// EndSwap releases the station after the rider's swap
// has completed or failed.
func (s *Station) EndSwap() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.swapInProgress = false
	s.swapRiderID = ""
}

// IsSwapInProgress reports whether a rider currently owns
// the swap session.
func (s *Station) IsSwapInProgress() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.swapInProgress
}

// FindEmptySlot returns the first slot that is currently
// completely empty and available for a returned battery.
//
// A copy is returned so callers cannot modify the station
// without going through the station/state-machine methods.
func (s *Station) FindEmptySlot() *Slot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.Slots {
		slot := s.Slots[i]

		if slot.State != SlotEmpty {
			continue
		}

		if slot.Battery != nil {
			continue
		}

		if slot.Occupied {
			continue
		}

		if !slot.Online {
			continue
		}

		return &slot
	}

	return nil
}

// FindChargedBatterySlot returns the first available station
// battery whose SOC is at least 90%.
//
// Only a battery that is physically stored inside a locked
// slot is eligible to be given to the rider.
func (s *Station) FindChargedBatterySlot() *Slot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.Slots {
		slot := s.Slots[i]

		if slot.State != SlotLocked {
			continue
		}

		if slot.Battery == nil {
			continue
		}

		if !slot.Occupied {
			continue
		}

		if !slot.Online {
			continue
		}

		if slot.Battery.ChargePercentage < 90 {
			continue
		}

		// Return a safe copy.
		battery := *slot.Battery
		slot.Battery = &battery

		return &slot
	}

	return nil
}
