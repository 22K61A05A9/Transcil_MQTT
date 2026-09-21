//Now create the cabinet(station) itself.
package station

import "sync"

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

// GetSlot returns a safe copy of the requested slot.
//
// The returned slot is a copy, so callers cannot accidentally
// modify the station state without going through the state machine.
func (s *Station) GetSlot(number int) *Slot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if number < 1 || number > len(s.Slots) {
		return nil
	}

	slot := s.Slots[number-1]

	// Deep-copy the battery pointer.
	if slot.Battery != nil {
		battery := *slot.Battery
		slot.Battery = &battery
	}

	return &slot
}

// Snapshot returns a consistent read-only copy of the complete
// station state.
//
// Heartbeat and response-building code should use Snapshot()
// instead of directly reading mutable station fields.
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

// getSlot returns the actual internal slot.
//
// IMPORTANT:
// This method must only be called while s.mu is already locked.
func (s *Station) getSlot(number int) *Slot {
	if number < 1 || number > len(s.Slots) {
		return nil
	}

	return &s.Slots[number-1]
}
