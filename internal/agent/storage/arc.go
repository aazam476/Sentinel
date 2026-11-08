// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aazam476/sentinel/internal/protocol"
)

// arcStatsPath is where Linux's ZFS module publishes ARC statistics.
const arcStatsPath = "/proc/spl/kstat/zfs/arcstats"

// ParseARCStats reads /proc/spl/kstat/zfs/arcstats: a kstat header line,
// a "name type data" line, then one "name type value" line per statistic.
func ParseARCStats(content []byte) (protocol.ZFSARC, error) {
	values := map[string]uint64{}
	for line := range strings.SplitSeq(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		if value, err := strconv.ParseUint(fields[2], 10, 64); err == nil {
			values[fields[0]] = value
		}
	}
	for _, needed := range []string{"size", "c_max", "hits", "misses"} {
		if _, found := values[needed]; !found {
			return protocol.ZFSARC{}, fmt.Errorf("read ARC statistics: %s is missing", needed)
		}
	}
	return protocol.ZFSARC{
		SizeBytes: values["size"], TargetMaxBytes: values["c_max"], Hits: values["hits"], Misses: values["misses"],
	}, nil
}
