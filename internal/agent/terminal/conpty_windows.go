// SPDX-License-Identifier: Apache-2.0

//go:build windows

package terminal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NewStarter returns the Starter for this machine: Windows PowerShell in a
// pseudo-console (ConPTY), the terminal Windows Terminal itself uses.
func NewStarter() Starter {
	return func(cols, rows uint16) (Shell, error) {
		return startConPTY(defaultShell(), cols, rows)
	}
}

// defaultShell is Windows PowerShell, which every supported Windows has,
// by full path so a program named powershell.exe elsewhere on PATH can't
// stand in for it.
func defaultShell() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe") + " -NoLogo"
}

// startConPTY runs commandLine in a new pseudo-console of the given size.
func startConPTY(commandLine string, cols, rows uint16) (Shell, error) {
	var inputRead, inputWrite, outputRead, outputWrite windows.Handle
	if err := windows.CreatePipe(&inputRead, &inputWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("create input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outputRead, &outputWrite, nil, 0); err != nil {
		closeHandles(inputRead, inputWrite)
		return nil, fmt.Errorf("create output pipe: %w", err)
	}
	var console windows.Handle
	err := windows.CreatePseudoConsole(coord(cols, rows), inputRead, outputWrite, 0, &console)
	// The console keeps its own copies of its ends of the pipes.
	closeHandles(inputRead, outputWrite)
	if err != nil {
		closeHandles(inputWrite, outputRead)
		return nil, fmt.Errorf("create pseudo-console: %w", err)
	}
	process, err := startInConsole(commandLine, console)
	if err != nil {
		windows.ClosePseudoConsole(console)
		closeHandles(inputWrite, outputRead)
		return nil, err
	}
	return &conPTYShell{
		console: console, process: process,
		input: os.NewFile(uintptr(inputWrite), "conpty-input"), output: os.NewFile(uintptr(outputRead), "conpty-output"),
	}, nil
}

// startInConsole starts commandLine attached to console.
func startInConsole(commandLine string, console windows.Handle) (windows.Handle, error) {
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, fmt.Errorf("create attribute list: %w", err)
	}
	defer attributes.Delete()
	// UpdateProcThreadAttribute is called directly: for a pseudo-console the
	// attribute's value is the console handle itself, not a pointer to it,
	// which x/sys's Update (taking an unsafe.Pointer) can't express safely.
	update := windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")
	list := uintptr(unsafe.Pointer(attributes.List())) //nolint:gosec // G103: the attribute list's address, as the Win32 call takes it
	result, _, callErr := update.Call(list, 0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, uintptr(console), unsafe.Sizeof(console), 0, 0)
	if result == 0 {
		return 0, fmt.Errorf("attach pseudo-console: %w", callErr)
	}
	startup := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	command, err := windows.UTF16PtrFromString(commandLine)
	if err != nil {
		return 0, fmt.Errorf("encode command line: %w", err)
	}
	var info windows.ProcessInformation
	err = windows.CreateProcess(nil, command, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT, nil, nil, &startup.StartupInfo, &info)
	if err != nil {
		return 0, fmt.Errorf("start %s: %w", commandLine, err)
	}
	closeHandles(info.Thread)
	return info.Process, nil
}

// conPTYShell is a shell in a pseudo-console.
type conPTYShell struct {
	console windows.Handle
	process windows.Handle
	input   *os.File
	output  *os.File
}

func (s *conPTYShell) Read(buffer []byte) (int, error) {
	n, err := s.output.Read(buffer)
	// A closed pipe is how the console reports that it ended.
	if err != nil && !errors.Is(err, io.EOF) {
		err = io.EOF
	}
	return n, err
}

func (s *conPTYShell) Write(data []byte) (int, error) {
	return s.input.Write(data)
}

func (s *conPTYShell) Resize(cols, rows uint16) error {
	return windows.ResizePseudoConsole(s.console, coord(cols, rows))
}

// Close ends the shell and the console; closing the console ends the
// output pipe, which ends the session's reader.
func (s *conPTYShell) Close() error {
	_ = windows.TerminateProcess(s.process, 1) // it may have exited already
	windows.ClosePseudoConsole(s.console)
	closeHandles(s.process)
	return errors.Join(s.input.Close(), s.output.Close())
}

func coord(cols, rows uint16) windows.Coord {
	return windows.Coord{X: int16(min(cols, 32767)), Y: int16(min(rows, 32767))}
}

func closeHandles(handles ...windows.Handle) {
	for _, handle := range handles {
		_ = windows.CloseHandle(handle) // closing a handle we own; nothing to do if it fails
	}
}
