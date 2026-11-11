// SPDX-License-Identifier: Apache-2.0

package tray

import (
	"fmt"
	"net/url"
	"time"
)

// Icon is the tray icon's state.
type Icon int

// The icon's states.
const (
	// IconOff: the agent isn't running, or isn't connected to its server.
	IconOff Icon = iota
	// IconIdle: connected, no one is in.
	IconIdle
	// IconActive: a remote session is open.
	IconActive
	// IconPaused: the person at the machine paused remote access.
	IconPaused
)

// Actions the person can pick from the menu.
const (
	ActionEnd    = "end"
	ActionPause  = "pause"
	ActionResume = "resume"
)

// Action is something the person picked from the menu.
type Action struct {
	Kind string
	// Session is the session to end, for ActionEnd.
	Session string
}

// Item is one line of the menu. An item without an Action is a label; an
// empty Label is a separator.
type Item struct {
	Label  string
	Action *Action
}

// Model is everything the tray shows.
type Model struct {
	Icon    Icon
	Tooltip string
	Items   []Item
}

// Menu builds what the tray shows from the agent's status. reachable is
// whether the tray could reach the agent at all; now places the sessions'
// start times in the local day.
func Menu(status Status, reachable bool, now time.Time) Model {
	if !reachable {
		return Model{Icon: IconOff, Tooltip: "Sentinel: the agent isn't running",
			Items: []Item{{Label: "The Sentinel agent isn't running"}}}
	}
	server := serverName(status.Server)
	model := Model{Icon: IconIdle, Tooltip: "Sentinel: connected to " + server}
	connection := "Connected to " + server
	if !status.Connected {
		model.Icon, model.Tooltip = IconOff, "Sentinel: not connected"
		connection = "Not connected to " + server + "; retrying"
	}
	model.Items = append(model.Items, Item{Label: connection})
	if status.ConsentRequired {
		model.Items = append(model.Items, Item{Label: "Remote desktop asks you first"})
	}
	if len(status.Sessions) > 0 {
		model.Icon, model.Tooltip = IconActive, "Sentinel: "+sessionCount(len(status.Sessions))+" open"
		model.Items = append(model.Items, Item{})
		for _, session := range status.Sessions {
			model.Items = append(model.Items, Item{
				Label:  "End " + describe(session, now),
				Action: &Action{Kind: ActionEnd, Session: session.ID},
			})
		}
	}
	model.Items = append(model.Items, Item{})
	if status.Paused {
		model.Icon, model.Tooltip = IconPaused, "Sentinel: remote access paused"
		model.Items = append(model.Items, Item{Label: "Resume remote access", Action: &Action{Kind: ActionResume}})
	} else {
		model.Items = append(model.Items, Item{Label: "Pause remote access", Action: &Action{Kind: ActionPause}})
	}
	if version := status.Version; version != "" {
		model.Items = append(model.Items, Item{Label: "Sentinel agent " + version})
	}
	return model
}

// serverName is the server's host, or its URL as given if it has none.
func serverName(server string) string {
	if parsed, err := url.Parse(server); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	if server == "" {
		return "the server"
	}
	return server
}

func sessionCount(count int) string {
	if count == 1 {
		return "1 remote session"
	}
	return fmt.Sprintf("%d remote sessions", count)
}

// describe names a session for the menu: "admin's terminal (since 14:02)".
func describe(session Session, now time.Time) string {
	who := session.User
	if who == "" {
		who = "someone"
	}
	what := "terminal"
	if session.Kind == KindDesktop {
		what = "view of this screen"
	}
	since := session.Since.In(now.Location())
	when := since.Format("15:04")
	if y, m, d := since.Date(); y != now.Year() || m != now.Month() || d != now.Day() {
		when = since.Format("Jan 2 15:04")
	}
	return fmt.Sprintf("%s's %s (since %s)", who, what, when)
}

// announcement is the notification for a session that just opened.
func announcement(session Session) string {
	who := session.User
	if who == "" {
		who = "Someone"
	}
	if session.Kind == KindDesktop {
		return who + " is connected to this screen through Sentinel"
	}
	return who + " opened a terminal on this machine through Sentinel"
}
