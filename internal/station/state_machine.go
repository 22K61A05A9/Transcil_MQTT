// we'll create the component responsible for changing physical state.
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

	// A slot should not be opened if it is already occupied
	// and locked.
	if slot.State == SlotLocked {
		return fmt.Errorf(
			"slot %d is already locked with a battery",
			slotNumber,
		)
	}

	slot.State = SlotDoorOpen
	slot.DoorOpen = true

	return nil
}

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
