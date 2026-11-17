// SPDX-License-Identifier: Apache-2.0

package policy

import "testing"

func TestSEC22_OnWindowsOnlyAdministratorsMayOwnOrChangeThePolicyFile(t *testing.T) {
	const installer = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
	cases := []struct {
		name    string
		sddl    string
		trusted bool
	}{
		{"as the installer leaves it", "O:BAG:SYD:P(A;;FA;;;SY)(A;;FA;;;BA)", true},
		{"with users allowed to read it", "O:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;BU)", true},
		{"owned by SYSTEM, by its SID", "O:S-1-5-18D:(A;;0x1F01FF;;;SY)", true},
		{"owned by TrustedInstaller", "O:" + installer + "D:(A;;FA;;;" + installer + ")", true},
		{"with an inherit-only entry for whoever creates files", "O:BAD:(A;;FA;;;BA)(A;OICIIO;GA;;;CO)", true},
		{"with a denial for everyone", "O:BAD:(D;;FW;;;WD)(A;;FA;;;BA)", true},
		{"owned by a standard user", "O:S-1-5-21-1-2-3-1001D:P(A;;FA;;;SY)(A;;FA;;;BA)", false},
		{"owned by the Users group", "O:BUD:P(A;;FA;;;BA)", false},
		{"writable by Users", "O:BAD:(A;;FA;;;BA)(A;;FW;;;BU)", false},
		{"writable by everyone, as a number", "O:BAD:(A;;FA;;;BA)(A;;0x120116;;;WD)", false},
		{"whose access list anyone can change", "O:BAD:(A;;FA;;;BA)(A;;RCWD;;;AU)", false},
		{"which a standard user can delete", "O:BAD:(A;;FA;;;BA)(A;;SD;;;S-1-5-21-1-2-3-1001)", false},
		{"with no access list at all", "O:BAD:NO_ACCESS_CONTROL", false},
		{"with no owner", "D:(A;;FA;;;BA)", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkSDDL(`C:\ProgramData\Sentinel\etc\policy.toml`, c.sddl)
			if (err == nil) != c.trusted {
				t.Errorf("checkSDDL(%s) = %v, want trusted %v", c.sddl, err, c.trusted)
			}
		})
	}
}
