// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// trustedSIDs are the accounts that may own or change the policy file on
// Windows: SYSTEM, the Administrators group, and TrustedInstaller.
func trustedSIDs() []string {
	return []string{"SY", "S-1-5-18", "BA", "S-1-5-32-544", "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"}
}

// writeRights are the SDDL rights, as letters, that let an account change
// a file, its access list or its owner, or delete it.
func writeRights() []string {
	return []string{"FA", "FW", "GA", "GW", "WD", "WO", "SD", "WP", "CC", "DC"}
}

// writeMask is the same as a numeric access mask: FILE_WRITE_DATA,
// FILE_APPEND_DATA, FILE_WRITE_EA, FILE_WRITE_ATTRIBUTES, DELETE, WRITE_DAC,
// WRITE_OWNER, GENERIC_ALL and GENERIC_WRITE.
const writeMask = 0x2 | 0x4 | 0x10 | 0x100 | 0x10000 | 0x40000 | 0x80000 | 0x10000000 | 0x40000000

var (
	sddlOwner = regexp.MustCompile(`^O:([A-Z]{2}|S-[0-9-]+)`)
	sddlACE   = regexp.MustCompile(`\(([^;()]*);([^;()]*);([^;()]*);[^;()]*;[^;()]*;([^;()]*)\)`)
)

// checkSDDL refuses a policy file, described in SDDL, that anyone but
// SYSTEM, Administrators or TrustedInstaller owns or could change: they
// could switch capabilities back on (SEC-22).
func checkSDDL(path, sddl string) error {
	owner := sddlOwner.FindStringSubmatch(sddl)
	if owner == nil || !isTrustedSID(owner[1]) {
		return fmt.Errorf("policy file %s must be owned by SYSTEM or Administrators; run: icacls %q /setowner Administrators", path, path)
	}
	dacl := sddl[strings.Index(sddl, "D:")+2:]
	if !strings.Contains(sddl, "D:") || strings.HasPrefix(dacl, "NO_ACCESS_CONTROL") {
		return fmt.Errorf("policy file %s has no access list, so anyone can change it", path)
	}
	if end := strings.Index(dacl, "S:"); end >= 0 {
		dacl = dacl[:end]
	}
	for _, ace := range sddlACE.FindAllStringSubmatch(dacl, -1) {
		kind, flags, rights, sid := ace[1], ace[2], ace[3], ace[4]
		if kind != "A" || strings.Contains(flags, "IO") || isTrustedSID(sid) {
			continue // denials, inherit-only entries and trusted accounts
		}
		if grantsWrite(rights) {
			return fmt.Errorf("policy file %s can be changed by %s; only SYSTEM and Administrators may: run: icacls %q /inheritance:r /grant:r SYSTEM:F Administrators:F", path, sid, path)
		}
	}
	return nil
}

func isTrustedSID(sid string) bool {
	for _, trusted := range trustedSIDs() {
		if strings.EqualFold(sid, trusted) {
			return true
		}
	}
	return false
}

// grantsWrite reads rights as either a number or pairs of letters.
func grantsWrite(rights string) bool {
	if mask, err := strconv.ParseUint(rights, 0, 32); err == nil {
		return mask&writeMask != 0
	}
	for index := 0; index+2 <= len(rights); index += 2 {
		for _, right := range writeRights() {
			if rights[index:index+2] == right {
				return true
			}
		}
	}
	return false
}
