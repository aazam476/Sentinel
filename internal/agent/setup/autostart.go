// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"encoding/xml"
)

// launchAgentLabel names the tray's launch agent on macOS.
const launchAgentLabel = "com.sentinel.tray"

// launchAgentPlist starts `executable tray` in each graphical (Aqua)
// session, and again if it crashes.
func launchAgentPlist(executable string) string {
	var path bytes.Buffer
	_ = xml.EscapeText(&path, []byte(executable)) // a bytes.Buffer doesn't fail
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + launchAgentLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + path.String() + `</string>
		<string>tray</string>
	</array>
	<key>LimitLoadToSessionType</key>
	<string>Aqua</string>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
</dict>
</plist>
`
}

// runCommand is how Windows starts the tray: the Run entry's value.
func runCommand(executable string) string {
	return `"` + executable + `" tray`
}
