// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/aazam476/sentinel/internal/protocol"
)

// lsblkColumns are the columns asked of lsblk. PATH would be simpler but
// needs util-linux 2.33; the path is built from NAME instead.
const lsblkColumns = "NAME,MODEL,SERIAL,SIZE,ROTA,TRAN,TYPE"

// flexible is a JSON value that older lsblk and PowerShell print as a
// string and newer ones as a number or boolean.
type flexible string

func (f *flexible) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*f = flexible(strings.TrimSpace(text))
		return nil
	}
	if string(data) == "null" {
		*f = ""
		return nil
	}
	*f = flexible(strings.TrimSpace(string(data)))
	return nil
}

func (f flexible) uint() uint64 {
	value, err := strconv.ParseUint(string(f), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

type lsblkDevice struct {
	Name     flexible      `json:"name"`
	Model    flexible      `json:"model"`
	Serial   flexible      `json:"serial"`
	Size     flexible      `json:"size"`
	Rota     flexible      `json:"rota"`
	Tran     flexible      `json:"tran"`
	Type     flexible      `json:"type"`
	Children []lsblkDevice `json:"children"`
}

// DiskIndex maps every block device name (a disk or any partition on it)
// to the disk's name, to find which disk a pool member is on.
type DiskIndex map[string]string

// notPhysical are prefixes of block devices that lsblk calls disks but
// aren't: zvols (zd), compressed RAM, RAM disks, network block devices.
func notPhysical() []string { return []string{"zd", "zram", "ram", "nbd", "loop"} }

// ParseLsblk reads `lsblk -J -b -o <lsblkColumns>`.
func ParseLsblk(output []byte) ([]protocol.PhysicalDisk, DiskIndex, error) {
	var parsed struct {
		Devices []lsblkDevice `json:"blockdevices"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, nil, fmt.Errorf("read lsblk: %w", err)
	}
	disks, index := []protocol.PhysicalDisk{}, DiskIndex{}
	for _, device := range parsed.Devices {
		name := string(device.Name)
		if device.Type != "disk" || name == "" || hasAnyPrefix(name, notPhysical()) {
			continue
		}
		disks = append(disks, protocol.PhysicalDisk{
			Name: name, Path: "/dev/" + name, Model: string(device.Model), Serial: string(device.Serial),
			SizeBytes: device.Size.uint(), Kind: lsblkKind(device),
		})
		indexDevice(index, device, name)
	}
	return disks, index, nil
}

func indexDevice(index DiskIndex, device lsblkDevice, disk string) {
	index[string(device.Name)] = disk
	for _, child := range device.Children {
		indexDevice(index, child, disk)
	}
}

func lsblkKind(device lsblkDevice) string {
	switch {
	case device.Tran == "nvme" || strings.HasPrefix(string(device.Name), "nvme"):
		return "nvme"
	case device.Rota == "true" || device.Rota == "1":
		return "hdd"
	case device.Rota == "false" || device.Rota == "0":
		return "ssd"
	}
	return "unknown"
}

func hasAnyPrefix(text string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// windowsDisksScript lists physical disks as JSON, always as an array.
// The enum fields come out as numbers or names depending on the Windows
// version, so the parser accepts both.
const windowsDisksScript = `$ErrorActionPreference = 'Stop'
$disks = @(Get-PhysicalDisk | ForEach-Object {
  $counter = $_ | Get-StorageReliabilityCounter -ErrorAction SilentlyContinue
  [pscustomobject]@{
    DeviceId = [string]$_.DeviceId; FriendlyName = $_.FriendlyName; SerialNumber = $_.SerialNumber
    Size = $_.Size; MediaType = [string]$_.MediaType; BusType = [string]$_.BusType
    HealthStatus = [string]$_.HealthStatus; Temperature = $counter.Temperature
  }
})
ConvertTo-Json -InputObject $disks -Compress`

type windowsDisk struct {
	DeviceID     flexible `json:"DeviceId"`
	FriendlyName flexible `json:"FriendlyName"`
	SerialNumber flexible `json:"SerialNumber"`
	Size         flexible `json:"Size"`
	MediaType    flexible `json:"MediaType"`
	BusType      flexible `json:"BusType"`
	HealthStatus flexible `json:"HealthStatus"`
	Temperature  flexible `json:"Temperature"`
}

// ParseWindowsDisks reads windowsDisksScript's output. Windows' own disk
// health stands in for SMART.
func ParseWindowsDisks(output []byte) ([]protocol.PhysicalDisk, error) {
	var parsed []windowsDisk
	if trimmed := bytes.TrimSpace(output); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &parsed); err != nil {
			return nil, fmt.Errorf("read Get-PhysicalDisk: %w", err)
		}
	}
	disks := []protocol.PhysicalDisk{}
	for _, disk := range parsed {
		result := protocol.PhysicalDisk{
			Name: "PhysicalDrive" + string(disk.DeviceID), Path: `\\.\PhysicalDrive` + string(disk.DeviceID),
			Model: string(disk.FriendlyName), Serial: string(disk.SerialNumber), SizeBytes: disk.Size.uint(),
			Kind: windowsKind(disk),
		}
		if health := windowsHealth(disk); health != nil {
			result.SMART = health
		}
		disks = append(disks, result)
	}
	return disks, nil
}

// windowsKind reads MSFT_PhysicalDisk's BusType (17 is NVMe) and
// MediaType (3 is HDD, 4 is SSD).
func windowsKind(disk windowsDisk) string {
	switch {
	case disk.BusType == "NVMe" || disk.BusType == "17":
		return "nvme"
	case disk.MediaType == "SSD" || disk.MediaType == "4":
		return "ssd"
	case disk.MediaType == "HDD" || disk.MediaType == "3":
		return "hdd"
	}
	return "unknown"
}

// windowsHealth reads HealthStatus: Healthy (0), Warning (1) or
// Unhealthy (2). Anything else is unknown.
func windowsHealth(disk windowsDisk) *protocol.DiskSMART {
	health := &protocol.DiskSMART{Failing: []string{}, Warnings: []string{}}
	switch disk.HealthStatus {
	case "Healthy", "0":
		health.Passed = true
	case "Warning", "1":
		health.Passed, health.Warnings = true, []string{"Windows reports a warning for this disk"}
	case "Unhealthy", "2":
		health.Failing = []string{"Windows reports this disk as unhealthy"}
	default:
		return nil
	}
	if celsius := disk.Temperature.uint(); celsius > 0 {
		value := float64(celsius)
		health.TemperatureCelsius = &value
	}
	return health
}
