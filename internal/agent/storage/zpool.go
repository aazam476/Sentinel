// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
)

// poolListColumns are the columns asked of `zpool list`, in order.
const poolListColumns = "name,size,alloc,free,frag,dedupratio,health"

// ParsePoolList reads `zpool list -H -p -o name,size,alloc,free,frag,
// dedupratio,health`: one tab-separated line per pool.
func ParsePoolList(output []byte) ([]protocol.ZFSPool, error) {
	pools := []protocol.ZFSPool{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			return nil, fmt.Errorf("read zpool list: expected 7 columns, got %d in %q", len(fields), line)
		}
		pool := protocol.ZFSPool{Name: fields[0], Health: fields[6], Vdevs: []protocol.ZFSVdev{}}
		var err error
		if pool.SizeBytes, err = parseSize(fields[1]); err != nil {
			return nil, fmt.Errorf("read zpool list size of %s: %w", pool.Name, err)
		}
		if pool.AllocatedBytes, err = parseSize(fields[2]); err != nil {
			return nil, fmt.Errorf("read zpool list allocation of %s: %w", pool.Name, err)
		}
		if pool.FreeBytes, err = parseSize(fields[3]); err != nil {
			return nil, fmt.Errorf("read zpool list free space of %s: %w", pool.Name, err)
		}
		if fragmentation, err := parseSize(strings.TrimSuffix(fields[4], "%")); err == nil && fields[4] != "-" {
			pool.FragmentationPercent = &fragmentation
		}
		pool.DedupRatio = parseRatio(fields[5])
		pools = append(pools, pool)
	}
	return pools, nil
}

// PoolStatus is the part of `zpool status` for one pool.
type PoolStatus struct {
	State      string
	Status     string
	Action     string
	Scan       protocol.ZFSScan
	DataErrors string
	Vdevs      []protocol.ZFSVdev
}

// statusKey matches a `zpool status` line that starts a field, such as
// "  pool: tank" or " state: ONLINE".
var statusKey = regexp.MustCompile(`^\s*(pool|state|status|action|scan|see|config|errors|remove|checkpoint):\s?(.*)$`)

// ParsePoolStatus reads `zpool status -P -p`, which may describe several
// pools one after another. Times in it are local to the machine (loc).
func ParsePoolStatus(output []byte, loc *time.Location) (map[string]PoolStatus, error) {
	result := map[string]PoolStatus{}
	parser := statusParser{loc: loc, fields: map[string]string{}}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \r")
		if match := statusKey.FindStringSubmatch(line); match != nil {
			if match[1] == "pool" && parser.pool != "" {
				result[parser.pool] = parser.finish()
				parser = statusParser{loc: loc, fields: map[string]string{}}
			}
			parser.startField(match[1], match[2])
			continue
		}
		if err := parser.continueField(line); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read zpool status: %w", err)
	}
	if parser.pool != "" {
		result[parser.pool] = parser.finish()
	}
	return result, nil
}

// statusParser collects one pool's fields from `zpool status`.
type statusParser struct {
	loc     *time.Location
	pool    string
	current string
	fields  map[string]string
	config  configParser
}

func (p *statusParser) startField(key, value string) {
	p.current = key
	if key == "pool" {
		p.pool = strings.TrimSpace(value)
		return
	}
	p.fields[key] = strings.TrimSpace(value)
}

// continueField adds a line to the field it belongs to: a vdev line under
// config, or the continuation of a wrapped field such as status.
func (p *statusParser) continueField(line string) error {
	if p.current == "config" {
		return p.config.add(line)
	}
	if text := strings.TrimSpace(line); text != "" && p.current != "" && p.current != "pool" {
		p.fields[p.current] = strings.TrimSpace(p.fields[p.current] + " " + text)
	}
	return nil
}

func (p *statusParser) finish() PoolStatus {
	return PoolStatus{
		State:      p.fields["state"],
		Status:     p.fields["status"],
		Action:     p.fields["action"],
		Scan:       parseScan(p.fields["scan"], p.loc),
		DataErrors: p.fields["errors"],
		Vdevs:      p.config.vdevs(),
	}
}

// sectionNames are the headings `zpool status` puts before vdevs that
// aren't data vdevs, and what each section is called in the protocol.
func sectionNames() map[string]string {
	return map[string]string{
		"logs": "log", "cache": "cache", "spares": "spare", "special": "special", "dedup": "dedup",
	}
}

// configParser builds the vdev tree from the config section, where depth
// is two spaces of indentation per level after a leading tab.
type configParser struct {
	sawHeader bool
	sawPool   bool
	section   string
	top       []protocol.ZFSVdev
	// path holds the index of each open ancestor below the top level.
	path []int
}

func (c *configParser) add(line string) error {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	fields := strings.Fields(line)
	if !c.sawHeader {
		c.sawHeader = fields[0] == "NAME"
		return nil
	}
	depth := indentDepth(line)
	if !c.sawPool {
		// The first line is the pool itself; its vdevs follow.
		if depth != 0 {
			return fmt.Errorf("read zpool status: expected the pool before %s", fields[0])
		}
		c.sawPool, c.section = true, "data"
		return nil
	}
	if depth == 0 {
		section, known := sectionNames()[fields[0]]
		if !known {
			return fmt.Errorf("read zpool status: unknown vdev section %q", fields[0])
		}
		c.section = section
		return nil
	}
	return c.place(parseVdevLine(fields, c.section), depth)
}

// place puts vdev at depth: 1 is a top-level vdev, deeper is a child of
// the last vdev one level up.
func (c *configParser) place(vdev protocol.ZFSVdev, depth int) error {
	if depth == 1 {
		c.top = append(c.top, vdev)
		c.path = []int{len(c.top) - 1}
		return nil
	}
	if depth-1 > len(c.path) || len(c.path) == 0 {
		return fmt.Errorf("read zpool status: %s is indented deeper than its parent", vdev.Name)
	}
	c.path = c.path[:depth-1]
	parent := &c.top[c.path[0]]
	for _, index := range c.path[1:] {
		parent = &parent.Children[index]
	}
	parent.Children = append(parent.Children, vdev)
	c.path = append(c.path, len(parent.Children)-1)
	return nil
}

func (c *configParser) vdevs() []protocol.ZFSVdev {
	if c.top == nil {
		return []protocol.ZFSVdev{}
	}
	return c.top
}

// indentDepth is the vdev depth of a config line: a leading tab, then two
// spaces per level.
func indentDepth(line string) int {
	trimmed := strings.TrimPrefix(line, "\t")
	return (len(trimmed) - len(strings.TrimLeft(trimmed, " "))) / 2
}

// vdevKind matches the names zpool gives grouping vdevs, such as
// "mirror-0", "raidz2-1" or "draid1:4d:6c:1s-0".
var vdevKind = regexp.MustCompile(`^(mirror|raidz[123]?|draid[123]?|replacing|spare)(:[^-]*)?-\d+$`)

// parseVdevLine reads "name state read write cksum [note]". Spares show
// only "name state [note]".
func parseVdevLine(fields []string, section string) protocol.ZFSVdev {
	vdev := protocol.ZFSVdev{Name: fields[0], Kind: "device", Section: section}
	if match := vdevKind.FindStringSubmatch(fields[0]); match != nil {
		vdev.Kind = match[1]
		if vdev.Kind == "raidz" {
			vdev.Kind = "raidz1"
		}
		if strings.HasPrefix(vdev.Kind, "draid") {
			vdev.Kind = "draid"
		}
	}
	if len(fields) > 1 {
		vdev.State = fields[1]
	}
	rest := fields[min(2, len(fields)):]
	if len(rest) >= 3 && isCount(rest[0]) && isCount(rest[1]) && isCount(rest[2]) {
		vdev.ReadErrors, vdev.WriteErrors, vdev.ChecksumErrors = parseCount(rest[0]), parseCount(rest[1]), parseCount(rest[2])
		rest = rest[3:]
	}
	vdev.Note = strings.Join(rest, " ")
	return vdev
}

// errorCount is an error count as zpool prints it: exact with -p, or
// abbreviated like "1.2K" without.
var errorCount = regexp.MustCompile(`^\d+(\.\d+)?[KMGTPE]?$`)

func isCount(field string) bool { return errorCount.MatchString(field) }

func parseCount(field string) uint64 {
	value, err := parseSize(field)
	if err != nil {
		return 0
	}
	return value
}

// The forms of zpool status's scan line that are understood.
var (
	scanFinished   = regexp.MustCompile(`^(scrub repaired|resilvered) \S+ in \S+ with (\d+) errors on (.+)$`)
	scanInProgress = regexp.MustCompile(`^(scrub|resilver) in progress since ([^\n]+?\d{4})`)
	scanPaused     = regexp.MustCompile(`^scrub paused since ([^\n]+?\d{4})`)
	scanCanceled   = regexp.MustCompile(`^(scrub|resilver) canceled on (.+?\d{4})`)
)

// parseScan reads the scan field. The time is in the machine's zone.
func parseScan(text string, loc *time.Location) protocol.ZFSScan {
	scan := protocol.ZFSScan{Kind: "none", State: "none", Text: text}
	switch {
	case text == "" || strings.HasPrefix(text, "none requested"):
	case scanFinished.MatchString(text):
		match := scanFinished.FindStringSubmatch(text)
		scan.Kind, scan.State, scan.Time = scanKind(match[1]), "finished", parseCTime(match[3], loc)
		scan.Errors, _ = strconv.ParseUint(match[2], 10, 64)
	case scanInProgress.MatchString(text):
		match := scanInProgress.FindStringSubmatch(text)
		scan.Kind, scan.State, scan.Time = match[1], "in_progress", parseCTime(match[2], loc)
	case scanPaused.MatchString(text):
		scan.Kind, scan.State, scan.Time = "scrub", "paused", parseCTime(scanPaused.FindStringSubmatch(text)[1], loc)
	case scanCanceled.MatchString(text):
		match := scanCanceled.FindStringSubmatch(text)
		scan.Kind, scan.State, scan.Time = match[1], "canceled", parseCTime(match[2], loc)
	default:
		scan.Kind, scan.State = "unknown", "unknown"
	}
	return scan
}

func scanKind(verb string) string {
	if verb == "resilvered" {
		return "resilver"
	}
	return "scrub"
}

// parseCTime reads a date as zpool prints it, "Sun Nov  8 00:24:01 2026",
// as UTC Unix milliseconds; 0 if it can't.
func parseCTime(text string, loc *time.Location) int64 {
	normalized := strings.Join(strings.Fields(text), " ")
	parsed, err := time.ParseInLocation("Mon Jan 2 15:04:05 2006", normalized, loc)
	if err != nil {
		return 0
	}
	return parsed.UnixMilli()
}
