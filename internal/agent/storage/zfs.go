// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/aazam476/sentinel/internal/protocol"
)

// datasetColumns are the columns asked of `zfs list`, in order. volsize
// is beyond what §9.5 lists, to show a zvol's size.
const datasetColumns = "name,used,avail,refer,usedbysnapshots,usedbychildren,quota,reservation,compressratio,mountpoint,type,volsize"

// ParseDatasets reads `zfs list -H -p -t filesystem,volume -o
// <datasetColumns>`: one tab-separated line per dataset, parents first.
func ParseDatasets(output []byte) ([]protocol.ZFSDataset, error) {
	datasets := []protocol.ZFSDataset{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 12 {
			return nil, fmt.Errorf("read zfs list: expected 12 columns, got %d in %q", len(fields), line)
		}
		dataset, err := parseDataset(fields)
		if err != nil {
			return nil, fmt.Errorf("read zfs list for %s: %w", fields[0], err)
		}
		datasets = append(datasets, dataset)
	}
	return datasets, nil
}

func parseDataset(fields []string) (protocol.ZFSDataset, error) {
	dataset := protocol.ZFSDataset{
		Name: fields[0], Type: fields[10], CompressRatio: parseRatio(fields[8]), Mountpoint: mountpoint(fields[9]),
	}
	sizes := []struct {
		field  string
		target *uint64
	}{
		{fields[1], &dataset.UsedBytes}, {fields[2], &dataset.AvailableBytes}, {fields[3], &dataset.ReferencedBytes},
		{fields[4], &dataset.SnapshotBytes}, {fields[5], &dataset.ChildrenBytes}, {fields[6], &dataset.QuotaBytes},
		{fields[7], &dataset.ReservationBytes}, {fields[11], &dataset.VolumeSizeBytes},
	}
	for _, size := range sizes {
		value, err := parseSize(size.field)
		if err != nil {
			return protocol.ZFSDataset{}, err
		}
		*size.target = value
	}
	if dataset.Type != "filesystem" && dataset.Type != "volume" {
		return protocol.ZFSDataset{}, fmt.Errorf("unknown dataset type %q", dataset.Type)
	}
	return dataset, nil
}

// mountpoint is where a dataset is mounted, or "" for "-" (a volume),
// "none" and "legacy" (mounted through fstab, not by ZFS).
func mountpoint(field string) string {
	switch field {
	case "-", "none", "legacy":
		return ""
	}
	return field
}

// parseSize reads a size or count. With -p, ZFS prints exact numbers,
// and "-" or "none" for values that don't apply; without -p (and in error
// counts) it abbreviates, as in "1.50M".
func parseSize(field string) (uint64, error) {
	switch field {
	case "-", "none", "":
		return 0, nil
	}
	if value, err := strconv.ParseUint(field, 10, 64); err == nil {
		return value, nil
	}
	suffixes := "BKMGTPE"
	unit := strings.IndexByte(suffixes, field[len(field)-1])
	if unit < 0 {
		return 0, fmt.Errorf("read size %q: not a number", field)
	}
	number, err := strconv.ParseFloat(field[:len(field)-1], 64)
	if err != nil || number < 0 || math.IsInf(number, 0) || math.IsNaN(number) {
		return 0, fmt.Errorf("read size %q: not a number", field)
	}
	value := number * math.Pow(1024, float64(unit))
	if value >= math.MaxUint64 {
		return 0, fmt.Errorf("read size %q: too large", field)
	}
	return uint64(value), nil
}

// parseRatio reads a ratio as ZFS prints it: "1.00", or "1.00x" without -p.
// Anything unreadable is 1, meaning no compression or deduplication.
func parseRatio(field string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSuffix(field, "x"), 64)
	if err != nil || value <= 0 || math.IsInf(value, 0) || math.IsNaN(value) {
		return 1
	}
	return value
}
