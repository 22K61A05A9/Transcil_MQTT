package protocol

type Calon11 struct {
	Date string
	Time string

	RiderID string

	SlotID int

	BatterySerial string
	BluetoothID   string

	HeartbeatAck    int
	SwapState       int
	OpenBatterySlot int
	Buzzer          int
}
