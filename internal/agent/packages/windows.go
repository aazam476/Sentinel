// SPDX-License-Identifier: Apache-2.0

package packages

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
)

// PowerShell runs the Windows Update scripts, which use its COM API.
const powershell = "powershell.exe"

// powershellArgs runs one script, without the user's profile or prompts.
func powershellArgs(script string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script}
}

// wingetPattern is where winget is installed for every user: SYSTEM, which
// the agent runs as, doesn't have it on its PATH.
const wingetPattern = `C:\Program Files\WindowsApps\Microsoft.DesktopAppInstaller_*_x64__8wekyb3d8bbwe\winget.exe`

// windowsManagers is Windows Update, and winget when it is installed.
func windowsManagers(host Host) []Manager {
	managers := []Manager{windowsUpdate{host.Runner}}
	if path := host.Find(wingetPattern); path != "" {
		managers = append(managers, winget{runner: host.Runner, path: path})
	}
	return managers
}

// windowsUpdate is Windows Update, through the Microsoft.Update.Session
// COM object.
type windowsUpdate struct{ runner command.Runner }

func (windowsUpdate) Name() string     { return "windows-update" }
func (windowsUpdate) Selectable() bool { return true }

// validID allows an update's ID, a GUID: it is put into a script.
func (windowsUpdate) validID(id string) bool { return guid.MatchString(id) }

var guid = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// searchScript lists the updates not yet installed, as JSON.
const searchScript = `$ErrorActionPreference = 'Stop'
$session = New-Object -ComObject Microsoft.Update.Session
$found = $session.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0 and Type='Software'")
$list = @($found.Updates | ForEach-Object {
  [pscustomobject]@{
    id = $_.Identity.UpdateID
    title = $_.Title
    security = [bool]($_.Categories | Where-Object { $_.Name -eq 'Security Updates' })
  }
})
ConvertTo-Json -InputObject $list -Compress`

// installScript downloads and installs the updates whose IDs are in
// $wanted (every one when it is empty) and says whether to restart.
const installScript = `$ErrorActionPreference = 'Stop'
$session = New-Object -ComObject Microsoft.Update.Session
$found = $session.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0 and Type='Software'")
$chosen = New-Object -ComObject Microsoft.Update.UpdateColl
foreach ($update in $found.Updates) {
  if ($wanted.Count -eq 0 -or $wanted -contains $update.Identity.UpdateID) {
    if (-not $update.EulaAccepted) { $update.AcceptEula() }
    [void]$chosen.Add($update)
  }
}
if ($chosen.Count -eq 0) { 'Nothing to install.'; exit 0 }
$downloader = $session.CreateUpdateDownloader(); $downloader.Updates = $chosen; [void]$downloader.Download()
$installer = $session.CreateUpdateInstaller(); $installer.Updates = $chosen
$result = $installer.Install()
for ($i = 0; $i -lt $chosen.Count; $i++) {
  '{0}: {1}' -f $chosen.Item($i).Title, $result.GetUpdateResult($i).ResultCode
}
if ($result.ResultCode -gt 3) { exit 1 }`

// rebootScript says whether Windows waits for a restart.
const rebootScript = `(New-Object -ComObject Microsoft.Update.SystemInfo).RebootRequired`

func (w windowsUpdate) List(ctx context.Context) ([]protocol.PackageUpdate, error) {
	output, err := w.runner.Run(ctx, powershell, powershellArgs(searchScript)...)
	if err != nil {
		return nil, err
	}
	return ParseWindowsUpdate(output)
}

func (w windowsUpdate) Apply(ctx context.Context, ids []string) ([]byte, error) {
	quoted := make([]string, len(ids))
	for index, id := range ids {
		quoted[index] = "'" + id + "'" // GUIDs, checked by validID
	}
	script := "$wanted = @(" + strings.Join(quoted, ",") + ")\n" + installScript
	return w.runner.Run(ctx, powershell, powershellArgs(script)...)
}

type windowsUpdateEntry struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Security bool   `json:"security"`
}

// ParseWindowsUpdate reads searchScript's JSON.
func ParseWindowsUpdate(output []byte) ([]protocol.PackageUpdate, error) {
	var entries []windowsUpdateEntry
	if text := strings.TrimSpace(string(output)); text != "" {
		if err := json.Unmarshal([]byte(text), &entries); err != nil {
			return nil, fmt.Errorf("read Windows Update's list: %w", err)
		}
	}
	updates := []protocol.PackageUpdate{}
	for _, entry := range entries {
		if guid.MatchString(entry.ID) {
			updates = append(updates, protocol.PackageUpdate{ID: entry.ID, Name: entry.Title, Security: entry.Security})
		}
	}
	return updates, nil
}

// winget is the Windows Package Manager.
type winget struct {
	runner command.Runner
	path   string
}

func (winget) Name() string           { return "winget" }
func (winget) Selectable() bool       { return true }
func (winget) validID(id string) bool { return wingetID.MatchString(id) }

// wingetID is a package identifier such as Microsoft.VisualStudioCode.
var wingetID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+-]*$`)

// wingetAgreements accepts the sources' terms and never asks.
func wingetAgreements() []string {
	return []string{"--accept-source-agreements", "--disable-interactivity"}
}

func (w winget) List(ctx context.Context) ([]protocol.PackageUpdate, error) {
	output, err := w.runner.Run(ctx, w.path, append([]string{"upgrade"}, wingetAgreements()...)...)
	if updates := ParseWinget(output); err == nil || len(updates) > 0 {
		return updates, nil // winget exits non-zero when some packages can't be checked
	}
	return nil, err
}

func (w winget) Apply(ctx context.Context, ids []string) ([]byte, error) {
	base := append([]string{"upgrade", "--silent", "--accept-package-agreements"}, wingetAgreements()...)
	if len(ids) == 0 {
		return w.runner.Run(ctx, w.path, append(base, "--all")...)
	}
	var output []byte
	for _, id := range ids {
		printed, err := w.runner.Run(ctx, w.path, append(base, "--exact", "--id", id)...)
		output = append(output, printed...)
		if err != nil {
			return output, err
		}
	}
	return output, nil
}

// ParseWinget reads `winget upgrade`'s table, whose columns are found from
// the heading: "Name  Id  Version  Available  Source".
func ParseWinget(output []byte) []protocol.PackageUpdate {
	updates := []protocol.PackageUpdate{}
	var columns []int
	for line := range strings.Lines(string(output)) {
		line = strings.TrimRight(lastSegment(line), "\r\n")
		if columns == nil {
			columns = wingetColumns(line)
			continue
		}
		cells := cut(line, columns)
		if cells == nil || strings.Trim(line, "-") == "" || !wingetID.MatchString(cells[1]) || len(cells[1]) > maxID {
			continue
		}
		updates = append(updates, protocol.PackageUpdate{ID: cells[1], Name: cells[0], Current: cells[2], Available: cells[3]})
	}
	return updates
}

// lastSegment drops winget's progress spinner, which it redraws with "\r".
func lastSegment(line string) string {
	trimmed := strings.TrimRight(line, "\r\n")
	if index := strings.LastIndexByte(trimmed, '\r'); index >= 0 {
		return trimmed[index+1:]
	}
	return trimmed
}

// wingetColumns returns where each column starts, in runes, if line is
// the heading.
func wingetColumns(line string) []int {
	runes := []rune(line)
	columns := []int{}
	for _, heading := range []string{"Name", "Id", "Version", "Available"} {
		index := strings.Index(line, heading)
		if index < 0 {
			return nil
		}
		columns = append(columns, utf8.RuneCountInString(line[:index]))
	}
	if columns[0] != 0 || len(runes) <= columns[3] {
		return nil
	}
	return columns
}

// cut splits a row at the columns, giving four trimmed cells.
func cut(line string, columns []int) []string {
	runes := []rune(line)
	if len(runes) <= columns[3] {
		return nil
	}
	cells := make([]string, 4)
	for index, start := range columns {
		end := len(runes)
		if index+1 < len(columns) {
			end = columns[index+1]
		}
		cells[index] = strings.TrimSpace(string(runes[start:end]))
	}
	if available := fields(cells[3]); len(available) > 0 {
		cells[3] = available[0] // the source follows
	}
	return cells
}
