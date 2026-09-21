// This component is responsible for changing the physical state
// of the battery swapping station.

package station

import "fmt"

type StateMachine struct {
	station *Station
}

func NewStateMachine(station *Station) *StateMachine {
	return &StateMachine{
		station: station,
	}
}

// OpenSlot opens a cabinet slot.
//
// Valid transitions:
//
//	EMPTY  -> DOOR_OPEN
//	LOCKED -> DOOR_OPEN
//
// The LOCKED -> DOOR_OPEN transition is required when a rider
// wants to remove the currently installed battery.
//
// Other states cannot be opened directly.
func (sm *StateMachine) OpenSlot(slotNumber int) error {
	sm.station.mu.Lock()
	defer sm.station.mu.Unlock()

	slot := sm.station.getSlot(slotNumber)

	if slot == nil {
		return fmt.Errorf(
			"slot %d does not exist",
			slotNumber,
		)
	}

	if slot.DoorOpen {
		return fmt.Errorf(
			"slot %d door is already open",
			slotNumber,
		)
	}

	if slot.State != SlotEmpty &&
		slot.State != SlotLocked {
		return fmt.Errorf(
			"slot %d cannot be opened from state %s",
			slotNumber,
			slot.State,
		)
	}

	slot.State = SlotDoorOpen
	slot.DoorOpen = true

	return nil
}

// DetectBattery places a battery into an open slot.
//
//	DOOR_OPEN -> BATTERY_DETECTED
func (sm *StateMachine) DetectBattery(
	slotNumber int,
	battery *Battery,
) error {
	sm.station.mu.Lock()
	defer sm.station.mu.Unlock()

	slot := sm.station.getSlot(slotNumber)

	if slot == nil {
		return fmt.Errorf(
			"slot %d does not exist",
			slotNumber,
		)
	}

	if battery == nil {
		return fmt.Errorf(
			"battery cannot be nil",
		)
	}

	if !slot.DoorOpen {
		return fmt.Errorf(
			"slot %d door is not open",
			slotNumber,
		)
	}

	if slot.Battery != nil {
		return fmt.Errorf(
			"slot %d already contains battery %s",
			slotNumber,
			slot.Battery.ID,
		)
	}

	slot.Battery = battery
	slot.Occupied = true
	slot.State = SlotBatteryDetected

	return nil
}

// SeatBattery confirms that the battery has been properly
// placed inside the slot.
//
//	BATTERY_DETECTED -> BATTERY_SEATED
func (sm *StateMachine) SeatBattery(
	slotNumber int,
) error {
	sm.station.mu.Lock()
	defer sm.station.mu.Unlock()

	slot := sm.station.getSlot(slotNumber)

	if slot == nil {
		return fmt.Errorf(
			"slot %d does not exist",
			slotNumber,
		)
	}

	if slot.Battery == nil {
		return fmt.Errorf(
			"slot %d has no battery",
			slotNumber,
		)
	}

	if slot.State != SlotBatteryDetected {
		return fmt.Errorf(
			"slot %d is not in BATTERY_DETECTED state",
			slotNumber,
		)
	}

	slot.State = SlotBatterySeated

	return nil
}

// LockSlot closes and locks a slot containing a battery.
//
//	BATTERY_SEATED -> LOCKED
func (sm *StateMachine) LockSlot(
	slotNumber int,
) error {
	sm.station.mu.Lock()
	defer sm.station.mu.Unlock()

	slot := sm.station.getSlot(slotNumber)

	if slot == nil {
		return fmt.Errorf(
			"slot %d does not exist",
			slotNumber,
		)
	}

	if slot.Battery == nil {
		return fmt.Errorf(
			"slot %d has no battery",
			slotNumber,
		)
	}

	if slot.State != SlotBatterySeated {
		return fmt.Errorf(
			"slot %d is not in BATTERY_SEATED state",
			slotNumber,
		)
	}

	slot.State = SlotLocked
	slot.DoorOpen = false
	slot.Occupied = true

	return nil
}

// RemoveBattery removes the currently installed battery
// from an open slot.
//
//	DOOR_OPEN -> EMPTY
//
// The returned Battery object represents the battery that
// has been physically removed from the cabinet.
func (sm *StateMachine) RemoveBattery(
	slotNumber int,
) (*Battery, error) {
	sm.station.mu.Lock()
	defer sm.station.mu.Unlock()

	slot := sm.station.getSlot(slotNumber)

	if slot == nil {
		return nil, fmt.Errorf(
			"slot %d does not exist",
			slotNumber,
		)
	}

	if slot.Battery == nil {
		return nil, fmt.Errorf(
			"slot %d has no battery",
			slotNumber,
		)
	}

	if !slot.DoorOpen {
		return nil, fmt.Errorf(
			"slot %d door is not open",
			slotNumber,
		)
	}

	removedBattery := slot.Battery

	slot.Battery = nil
	slot.Occupied = false
	slot.State = SlotEmpty
	slot.DoorOpen = false

	return removedBattery, nil
}
