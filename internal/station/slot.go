// A slot represents one physical battery compartment.
package station

type SlotState string

const (
	SlotEmpty           SlotState = "EMPTY"
	SlotDoorOpen        SlotState = "DOOR_OPEN"
	SlotBatteryDetected SlotState = "BATTERY_DETECTED"
	SlotBatterySeated   SlotState = "BATTERY_SEATED"
	SlotLocked          SlotState = "LOCKED"
)

type Slot struct {
	Number int

	State SlotState

	DoorOpen  bool
	Online    bool
	Occupied  bool
	DoorFault bool

	Battery *Battery
}
