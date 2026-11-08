// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aazam476/sentinel/internal/protocol"
)

// errNoSMART means smartctl ran but the disk reported no SMART health,
// as virtual disks and disks in standby don't.
var errNoSMART = errors.New("the disk reported no SMART health")

type smartReport struct {
	Device struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
	} `json:"device"`
	ModelName    string `json:"model_name"`
	SerialNumber string `json:"serial_number"`
	UserCapacity struct {
		Bytes uint64 `json:"bytes"`
	} `json:"user_capacity"`
	RotationRate *int `json:"rotation_rate"`
	SmartStatus  *struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	Temperature *struct {
		Current *float64 `json:"current"`
	} `json:"temperature"`
	ATAAttributes struct {
		Table []smartAttribute `json:"table"`
	} `json:"ata_smart_attributes"`
	NVMeLog *struct {
		CriticalWarning uint64 `json:"critical_warning"`
	} `json:"nvme_smart_health_information_log"`
}

type smartAttribute struct {
	Name  string `json:"name"`
	Flags struct {
		Prefailure bool `json:"prefailure"`
	} `json:"flags"`
	WhenFailed string `json:"when_failed"`
}

// ParseSMART reads `smartctl --json -i -H -A`. smartctl's exit status is a
// bit mask that is non-zero even for a readable report of a failing
// disk, so the report is read whatever the exit status was.
func ParseSMART(output []byte) (protocol.PhysicalDisk, error) {
	var report smartReport
	if err := json.Unmarshal(bytes.TrimSpace(output), &report); err != nil {
		return protocol.PhysicalDisk{}, fmt.Errorf("read smartctl: %w", err)
	}
	disk := protocol.PhysicalDisk{
		Name: strings.TrimPrefix(report.Device.Name, "/dev/"), Path: report.Device.Name, Model: report.ModelName,
		Serial: report.SerialNumber, SizeBytes: report.UserCapacity.Bytes, Kind: smartKind(report),
	}
	if report.SmartStatus == nil {
		return disk, errNoSMART
	}
	health := &protocol.DiskSMART{Passed: report.SmartStatus.Passed, Failing: []string{}, Warnings: []string{}}
	if report.Temperature != nil && report.Temperature.Current != nil {
		health.TemperatureCelsius = report.Temperature.Current
	}
	for _, attribute := range report.ATAAttributes.Table {
		switch {
		case attribute.WhenFailed == "now":
			health.Failing = append(health.Failing, attribute.Name)
		case attribute.WhenFailed == "past" && attribute.Flags.Prefailure:
			health.Warnings = append(health.Warnings, attribute.Name)
		}
	}
	if report.NVMeLog != nil && report.NVMeLog.CriticalWarning != 0 {
		health.Warnings = append(health.Warnings, fmt.Sprintf("NVMe critical warning 0x%02x", report.NVMeLog.CriticalWarning))
	}
	disk.SMART = health
	return disk, nil
}

func smartKind(report smartReport) string {
	switch {
	case report.Device.Protocol == "NVMe":
		return "nvme"
	case report.RotationRate == nil:
		return "unknown"
	case *report.RotationRate == 0:
		return "ssd"
	}
	return "hdd"
}

// ParseSMARTScan reads `smartctl --scan --json`: the devices smartctl can
// talk to, by path.
func ParseSMARTScan(output []byte) ([]string, error) {
	var scan struct {
		Devices []struct {
			Name string `json:"name"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output), &scan); err != nil {
		return nil, fmt.Errorf("read smartctl --scan: %w", err)
	}
	paths := []string{}
	for _, device := range scan.Devices {
		if device.Name != "" {
			paths = append(paths, device.Name)
		}
	}
	return paths, nil
}
