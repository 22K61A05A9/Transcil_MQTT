package protocol

type Calon01 struct {
	Cabinet CabinetStatus
	Slots   []SlotStatus
}

type CabinetStatus struct {
	Date string
	Time string

	StationID string
	MachineID string

	FireSense  string
	Power      string
	Temp       string
	WaterLevel string

	PhaseReadings [6]string

	GSMSignal string
	SlotCount int
}

type SlotStatus struct {
	SlotID string

	DoorStatus      string
	CabinetTemp     string
	CabinetOnline   string
	SlotOccupancy   string
	DoorMalfunction string

	BatterySerial string
	BluetoothID   string

	TotalVoltage string
	TotalCurrent string

	SOC string
	SOH string

	RemainingCapacity string
	CellTemp          string

	CFETStatus string
	DFETStatus string

	MaxCellVoltage string
	MinCellVoltage string

	MaxCellTemp string
	MinCellTemp string

	BMSAlarms string

	ChargerStatus string

	ChargingVoltage string
	ChargingCurrent string
	ChargerAlarms   string
}
