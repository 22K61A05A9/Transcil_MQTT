package protocol

type Calon02 struct {
	Cabinet CabinetStatus

	RiderID string

	SlotID int

	BatterySerial string
	BluetoothID   string

	HeartbeatAck        int
	SwapState           int
	EmptySlotOpenStatus int
	Buzzer              int

	Slot SlotStatus
}
