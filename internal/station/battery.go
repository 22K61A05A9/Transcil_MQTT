// A battery is the thing that occupies a slot.
package station

import (
	"context"
	"time"
)

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

// StartBatteryCharging starts the battery charging loop.
//
// Simulator rule:
//   - A battery with ChargeStatus == "1" is charging.
//   - Its SOC increases by 1% every minute.
//   - Charging stops when SOC reaches 100%.
//   - ChargeStatus becomes "2" when charging is complete.
func (s *Station) StartBatteryCharging(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.chargeBatteries()

		case <-ctx.Done():
			return
		}
	}
}

// chargeBatteries increases the SOC of all batteries that are
// currently charging.
//
// The station mutex protects the slots and battery data because
// the heartbeat and swap logic can read/change the same data.
func (s *Station) chargeBatteries() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.Slots {
		slot := &s.Slots[i]

		if slot.Battery == nil {
			continue
		}

		battery := slot.Battery

		// Only batteries marked as charging should increase SOC.
		if battery.ChargeStatus != "1" {
			continue
		}

		// Already fully charged.
		if battery.ChargePercentage >= 100 {
			battery.ChargePercentage = 100
			battery.ChargeStatus = "2"
			battery.ChargingCurrent = 0
			continue
		}

		// Increase SOC by 1% per minute.
		battery.ChargePercentage += 1

		// Prevent SOC from exceeding 100%.
		if battery.ChargePercentage >= 100 {
			battery.ChargePercentage = 100
			battery.ChargeStatus = "2"
			battery.ChargingCurrent = 0
		}
	}
}

// DrainBattery simulates the battery being used by a rider.
//
// Simulator rule:
//   - SOC decreases by 1% every minute.
//   - The battery stops at 0%.
//
// The battery passed to this function is the battery that has
// been removed from the station and is currently being used
// outside the cabinet.
func (s *Station) DrainBattery(
	ctx context.Context,
	battery *Battery,
) {
	if battery == nil {
		return
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if battery.ChargePercentage <= 0 {
				battery.ChargePercentage = 0
				return
			}

			battery.ChargePercentage -= 1

			if battery.ChargePercentage < 0 {
				battery.ChargePercentage = 0
			}

		case <-ctx.Done():
			return
		}
	}
}
