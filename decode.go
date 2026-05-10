/*
   rtldavis, an rtl-sdr receiver for Davis Instruments weather stations.
   Packet decoding based on rtldavis.py by Luc Heijst / Matthew Wall.

   Decodes raw ISS packet bytes into human-readable sensor readings.
   Formulas are taken directly from the companion rtldavis.py driver.
*/
package main

import (
	"fmt"
	"math"
)

// rainPerTipMM is the volume per rain gauge tip in mm.
// 0.2 mm for EU/UK buckets (Davis 6152UK), 0.254 mm (0.01 in) for US.
const rainPerTipMM = 0.2

// calcThermistorTemp converts a 10-bit ADC raw value to degrees Celsius
// using the Davis Instruments thermistor formula.
// Returns NaN for out-of-range (disconnected/open-circuit) sensors.
func calcThermistorTemp(raw10 float64) float64 {
	const a = 18.81099
	const b = 0.0009988027
	const s1 = 0.002783573
	const s2 = 0.0002509406
	denom := 1.0/raw10 - b
	if denom <= 0 {
		return math.NaN()
	}
	r := a / denom / 1000.0 // resistance in kΩ
	return 1.0/(s1+s2*math.Log(r)) - 273.0
}

// DecodePacket interprets 8 decoded ISS bytes and returns a human-readable
// string of sensor readings.  data is m.Data (already SwapBitOrder'd and
// preamble-stripped); it includes the 2 CRC bytes at the end.
//
// Packet layout (byte indices into m.Data):
//   [0]  bits7-4 = message type, bit3 = battery low, bits2-0 = station ID
//   [1]  wind speed raw (mph)
//   [2]  wind direction raw (0-255)
//   [3]  sensor data high byte
//   [4]  sensor data low byte
//   [5]  flags / gust index
//   [6]  CRC high
//   [7]  CRC low
func DecodePacket(data []byte) string {
	if len(data) < 6 {
		return "decode:short"
	}

	msgType := (data[0] >> 4) & 0xF
	battLow := (data[0] >> 3) & 0x1

	// Wind speed and direction are present in every packet.
	// Values are only valid when at least one is non-zero.
	windStr := ""
	windSpeedMph := int(data[1])
	windDirRaw := int(data[2])
	if windSpeedMph != 0 || windDirRaw != 0 {
		// Vantage Pro/Pro2 potentiometer formula.
		var windDir float64
		switch {
		case windDirRaw == 0:
			windDir = 5.0
		case windDirRaw == 255:
			windDir = 355.0
		default:
			windDir = 9.0 + float64(windDirRaw-1)*342.0/253.0
		}
		windStr = fmt.Sprintf(" Wind:%dmph@%.0f°", windSpeedMph, windDir)
	}

	battStr := ""
	if battLow != 0 {
		battStr = " Batt:LOW"
	}

	var sensor string
	switch msgType {

	case 2: // Supercap voltage (Vantage Vue only)
		raw := ((int(data[3]) << 2) + (int(data[4]) >> 6)) & 0x3FF
		if raw != 0x3FF {
			sensor = fmt.Sprintf("Supercap:%.2fV", float64(raw)/300.0)
		} else {
			sensor = "Supercap:none"
		}

	case 4: // UV index
		// sentinel 0x3FF = no sensor fitted
		raw := ((int(data[3]) << 2) + (int(data[4]) >> 6)) & 0x3FF
		if raw != 0x3FF {
			sensor = fmt.Sprintf("UV:%.1f", float64(raw)/50.0)
		} else {
			sensor = "UV:none"
		}

	case 5: // Rain rate
		// time_between_tips_raw uses bits from both bytes
		tbt := ((int(data[4]) & 0x30) << 4) + int(data[3])
		if tbt == 0x3FF {
			sensor = "RainRate:0.0mm/h" // 0x3FF = no rain
		} else if data[4]&0x40 == 0 {
			// Heavy rain: tbt is in units of 1/16 s
			tbtSec := float64(tbt) / 16.0
			rate := 3600.0 / tbtSec * rainPerTipMM
			sensor = fmt.Sprintf("RainRate:%.1fmm/h(heavy)", rate)
		} else {
			// Light rain: tbt is in whole seconds
			rate := 3600.0 / float64(tbt) * rainPerTipMM
			sensor = fmt.Sprintf("RainRate:%.1fmm/h", rate)
		}

	case 6: // Solar radiation
		// sentinel 0x3FE / 0x3FF = no sensor
		raw := ((int(data[3]) << 2) + (int(data[4]) >> 6)) & 0x3FF
		if raw < 0x3FE {
			sensor = fmt.Sprintf("Solar:%.0fW/m2", float64(raw)*1.757936)
		} else {
			sensor = "Solar:none"
		}

	case 7: // Solar cell / panel voltage (Vue only)
		raw := ((int(data[3]) << 2) + (int(data[4]) >> 6)) & 0x3FF
		if raw != 0x3FF {
			sensor = fmt.Sprintf("SolarPwr:%.2fV", float64(raw)/300.0)
		} else {
			sensor = "SolarPwr:none"
		}

	case 8: // Outside temperature
		// 12-bit raw value: upper 8 bits from pkt[3], upper 4 bits of pkt[4]
		// sentinel 0xFFC = no sensor
		tempRaw12 := (int(data[3]) << 4) + (int(data[4]) >> 4)
		if tempRaw12 == 0xFFC {
			sensor = "Temp:none"
		} else if data[4]&0x8 != 0 {
			// Digital sensor (SHT31 / newer Vue)
			tempF := float64(tempRaw12) / 10.0
			tempC := (tempF - 32.0) * 5.0 / 9.0
			sensor = fmt.Sprintf("Temp:%.1fC(%.1fF)", tempC, tempF)
		} else {
			// Analog NTC thermistor (VP2 ISS standard)
			tempC := calcThermistorTemp(float64(tempRaw12) / 4.0)
			if math.IsNaN(tempC) {
				sensor = "Temp:err"
			} else {
				tempF := tempC*9.0/5.0 + 32.0
				sensor = fmt.Sprintf("Temp:%.1fC(%.1fF)", tempC, tempF)
			}
		}

	case 9: // 10-minute average wind gust
		gustMph := int(data[3])
		gustIdx := int(data[5]) >> 4
		if gustMph != 0 || gustIdx != 0 {
			sensor = fmt.Sprintf("Gust10m:%dmph", gustMph)
		} else {
			sensor = "Gust10m:none"
		}

	case 0xA: // Outside humidity
		// 12-bit value: upper nibble of pkt[4] is high 4 bits, pkt[3] is low 8 bits
		humRaw := ((int(data[4]) >> 4) << 8) + int(data[3])
		if humRaw != 0 {
			var hum float64
			if data[4]&0x08 != 0 {
				hum = float64(humRaw) / 10.0 // digital sensor
			} else {
				hum = float64(humRaw)*-0.301 + 710.23 // analog sensor
			}
			sensor = fmt.Sprintf("Hum:%.1f%%", hum)
		} else {
			sensor = "Hum:none"
		}

	case 0xC: // Unknown type – log raw bytes for future decoding
		sensor = fmt.Sprintf("TypeC:[%02x %02x %02x]", data[3], data[4], data[5])

	case 0xE: // Rain collector tip count
		// sentinel 0x80 = no sensor; counter wraps at 127 (mask high bit)
		if data[3] != 0x80 {
			rainCount := int(data[3]) & 0x7F
			sensor = fmt.Sprintf("Rain:%d", rainCount)
		} else {
			sensor = "Rain:none"
		}

	default:
		sensor = fmt.Sprintf("Type%X:unknown", msgType)
	}

	return sensor + windStr + battStr
}
