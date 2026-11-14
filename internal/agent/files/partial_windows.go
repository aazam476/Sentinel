// SPDX-License-Identifier: Apache-2.0

package files

import "io/fs"

// openWithoutFollowing is nothing on Windows, which has no such flag for
// os.OpenFile: openPartial's comparison with the path's own entry catches
// a link there. Making a link to a file needs a privilege ordinary users
// don't have. https://learn.microsoft.com/en-us/windows/security/threat-protection/security-policy-settings/create-symbolic-links
const openWithoutFollowing = 0

// hasOtherNames isn't checked on Windows: a hard link can only be made by
// someone who may already write the file.
func hasOtherNames(fs.FileInfo) bool {
	return false
}
