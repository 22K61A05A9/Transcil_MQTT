// centralize topic names
package mqtt

import "fmt"

// station to server
const TelemetryBMSRaw = "telemetry/bms/raw"

func BMSAckTopic(stationCode string) string {
	return fmt.Sprintf("bms/ack/%s", stationCode)
}
