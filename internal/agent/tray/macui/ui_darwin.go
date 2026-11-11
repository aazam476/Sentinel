// SPDX-License-Identifier: Apache-2.0

package macui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"golang.org/x/sys/unix"

	"github.com/aazam476/sentinel/internal/agent/tray"
)

// AppKit constants.
const (
	activationPolicyAccessory = 1    // no Dock icon, no menu of its own
	variableStatusItemLength  = -1.0 // as wide as its content
	iconPoints                = 18   // menu bar icons are 18 points tall
	notifyTimeout             = 10 * time.Second
)

// ErrAlreadyRunning means this user's tray is already running. It is never
// reassigned.
var ErrAlreadyRunning = errors.New("the Sentinel tray is already running for this user")

// nsSize is NSSize.
type nsSize struct{ width, height float64 }

// UI is the menu bar item. Make one with New, then call Run on the main
// thread.
type UI struct {
	onAction func(tray.Action)
	lock     *os.File

	mutex   sync.Mutex
	model   tray.Model
	actions map[int]tray.Action
	// The AppKit objects, set by Run on the main thread.
	item, menu, target objc.ID
	images             map[tray.Icon]objc.ID
}

// New loads AppKit and makes the UI; onAction hears the person's picks.
// It fails with ErrAlreadyRunning if this user already has a tray.
func New(onAction func(tray.Action)) (*UI, error) {
	lock, err := lockInstance()
	if err != nil {
		return nil, err
	}
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		_ = lock.Close() // failing anyway
		return nil, fmt.Errorf("load AppKit: %w", err)
	}
	return &UI{onAction: onAction, lock: lock, model: tray.Menu(tray.Status{}, false, time.Now())}, nil
}

// lockInstance takes a lock in the user's own temporary folder, so a
// second tray for the same user stops.
func lockInstance() (*os.File, error) {
	path := filepath.Join(os.TempDir(), "sentinel-tray.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: the user's own temporary folder
	if err != nil {
		return nil, fmt.Errorf("check for another tray: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close() // not ours
		return nil, ErrAlreadyRunning
	}
	return file, nil
}

func sel(name string) objc.SEL { return objc.RegisterName(name) }

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func nsString(text string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), text)
}

// Run shows the menu bar item and runs AppKit until ctx ends. It must be
// called on the main thread.
func (u *UI) Run(ctx context.Context) error {
	defer func() { _ = u.lock.Close() }() // the process is ending
	pool := class("NSAutoreleasePool").Send(sel("new"))
	app := class("NSApplication").Send(sel("sharedApplication"))
	app.Send(sel("setActivationPolicy:"), activationPolicyAccessory)
	if err := u.build(); err != nil {
		return err
	}
	u.refresh()
	pool.Send(sel("drain"))
	stop := context.AfterFunc(ctx, func() {
		app.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("terminate:"), objc.ID(0), false)
	})
	defer stop()
	app.Send(sel("run"))
	return nil
}

// build makes the status item, its menu, the images and the target that
// hears the menu's clicks.
func (u *UI) build() error {
	targetClass, err := objc.RegisterClass("SentinelTrayTarget", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: sel("pick:"), Fn: func(_ objc.ID, _ objc.SEL, sender objc.ID) { u.pick(objc.Send[int](sender, sel("tag"))) }},
		{Cmd: sel("refresh:"), Fn: func(objc.ID, objc.SEL, objc.ID) { u.refresh() }},
	})
	if err != nil {
		return fmt.Errorf("register the menu's target: %w", err)
	}
	item := class("NSStatusBar").Send(sel("systemStatusBar")).Send(sel("statusItemWithLength:"), variableStatusItemLength)
	item.Send(sel("retain"))
	menu := class("NSMenu").Send(sel("alloc")).Send(sel("init"))
	menu.Send(sel("setAutoenablesItems:"), false)
	item.Send(sel("setMenu:"), menu)
	images := map[tray.Icon]objc.ID{}
	for _, state := range []tray.Icon{tray.IconOff, tray.IconIdle, tray.IconActive, tray.IconPaused} {
		image, err := makeImage(state)
		if err != nil {
			return err
		}
		images[state] = image
	}
	u.mutex.Lock()
	u.item, u.menu, u.images = item, menu, images
	u.target = objc.ID(targetClass).Send(sel("alloc")).Send(sel("init"))
	u.mutex.Unlock()
	return nil
}

// makeImage draws a state's icon at twice its size in points, for Retina
// screens.
func makeImage(state tray.Icon) (objc.ID, error) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, tray.IconImage(state, 2*iconPoints)); err != nil {
		return 0, fmt.Errorf("draw the menu bar icon: %w", err)
	}
	raw := encoded.Bytes()
	data := class("NSData").Send(sel("dataWithBytes:length:"), unsafe.Pointer(&raw[0]), uint64(len(raw))) //nolint:gosec // G103: AppKit copies the bytes
	image := class("NSImage").Send(sel("alloc")).Send(sel("initWithData:"), data)
	if image == 0 {
		return 0, errors.New("AppKit couldn't read the menu bar icon")
	}
	image.Send(sel("setSize:"), nsSize{width: iconPoints, height: iconPoints})
	return image, nil
}

// refresh shows the current model. It runs on the main thread.
func (u *UI) refresh() {
	u.mutex.Lock()
	model, item, menu, target := u.model, u.item, u.menu, u.target
	image := u.images[model.Icon]
	u.mutex.Unlock()
	if item == 0 {
		return
	}
	button := item.Send(sel("button"))
	button.Send(sel("setImage:"), image)
	button.Send(sel("setToolTip:"), nsString(model.Tooltip))
	actions := u.fillMenu(menu, target, model)
	u.mutex.Lock()
	u.actions = actions
	u.mutex.Unlock()
}

// fillMenu replaces the menu's items with the model's.
func (u *UI) fillMenu(menu, target objc.ID, model tray.Model) map[int]tray.Action {
	menu.Send(sel("removeAllItems"))
	actions := map[int]tray.Action{}
	for index, line := range model.Items {
		if line.Label == "" {
			menu.Send(sel("addItem:"), class("NSMenuItem").Send(sel("separatorItem")))
			continue
		}
		entry := class("NSMenuItem").Send(sel("alloc")).Send(sel("initWithTitle:action:keyEquivalent:"),
			nsString(line.Label), sel("pick:"), nsString(""))
		entry.Send(sel("setTarget:"), target)
		entry.Send(sel("setTag:"), index)
		entry.Send(sel("setEnabled:"), line.Action != nil)
		if line.Action != nil {
			actions[index] = *line.Action
		}
		menu.Send(sel("addItem:"), entry)
		entry.Send(sel("release")) // the menu holds it
	}
	return actions
}

// pick carries out a click on a menu item.
func (u *UI) pick(tag int) {
	u.mutex.Lock()
	action, ok := u.actions[tag]
	u.mutex.Unlock()
	if ok {
		u.onAction(action)
	}
}

// Update shows the model, from any goroutine.
func (u *UI) Update(model tray.Model) {
	u.mutex.Lock()
	u.model = model
	target := u.target
	u.mutex.Unlock()
	if target != 0 {
		target.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("refresh:"), objc.ID(0), false)
	}
}

// Ask shows a dialog until the person answers or ctx ends (SEC-23).
func (u *UI) Ask(ctx context.Context, question string) (bool, error) {
	output, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", dialogScript(question)).Output() //nolint:gosec // G204: a fixed program; the question is quoted
	if ctx.Err() != nil {
		return false, fmt.Errorf("the question went away: %w", context.Cause(ctx))
	}
	if err != nil {
		return false, nil //nolint:nilerr // Don't Allow is the cancel button: osascript exits with an error for it
	}
	return allowed(string(output)), nil
}

// Notify shows a notification.
func (u *UI) Notify(text string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()
		_ = exec.CommandContext(ctx, "/usr/bin/osascript", "-e", notificationScript(text)).Run() //nolint:gosec // G204: as for Ask
	}()
}
