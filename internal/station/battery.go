// A battery is the thing that occupies a slot.
package station

type Battery struct {
	ID          string
	BluetoothID string

	TotalVoltage float64
	TotalCurrent float64

	ChargePercentage float64
	HealthPercentage float64
	UsableCapacity   float64

	CellTemperature float64

	ChargeSwitch    bool
	DischargeSwitch bool

	HighestCellVoltage     float64
	LowestCellVoltage      float64
	HighestCellTemperature float64
	LowestCellTemperature  float64

	Alarm string

	ChargeStatus string

	ChargingVoltage float64
	ChargingCurrent float64
	ChargingAlarm   string
}
