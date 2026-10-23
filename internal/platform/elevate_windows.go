package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// elevatedHelperArg makes hull.exe act as its own UAC helper. Hull elevates
// itself instead of an encoded PowerShell command line, which antivirus
// heuristics (Bitdefender) block as malicious.
const elevatedHelperArg = "__elevated-helper"

// MaybeRunElevatedHelper runs the elevated operations and exits when this
// process is the UAC helper. It must run before cobra, so no update prompt or
// engine check can stall the hidden window.
func MaybeRunElevatedHelper() bool {
	if len(os.Args) < 3 || os.Args[1] != elevatedHelperArg {
		return false
	}
	result, ops := os.Args[2], os.Args[3:]
	if err := runElevatedOps(ops, windowsHostsPath); err != nil {
		_ = os.WriteFile(result, []byte(err.Error()), 0o600)
		os.Exit(1)
	}
	os.Exit(0)
	return true
}

// runElevated runs ops in hull.exe through one UAC prompt and waits for it.
// Each op is a name followed by its fixed number of arguments (see
// runElevatedOps).
func runElevated(ops ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	resultFile, err := os.CreateTemp("", "hull-elevated-*.txt")
	if err != nil {
		return err
	}
	result := resultFile.Name()
	_ = resultFile.Close()
	defer func() { _ = os.Remove(result) }()

	args := append([]string{elevatedHelperArg, result}, ops...)
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = syscall.EscapeArg(a)
	}
	code, err := shellRunAs(exe, strings.Join(quoted, " "))
	if errors.Is(err, windows.ERROR_CANCELLED) {
		return fmt.Errorf("the UAC prompt was declined")
	}
	if err != nil {
		return fmt.Errorf("starting the elevated helper: %w", err)
	}
	if code != 0 {
		if msg, _ := os.ReadFile(result); len(bytes.TrimSpace(msg)) > 0 {
			return errors.New(strings.TrimSpace(string(msg)))
		}
		return fmt.Errorf("the elevated helper exited with code %d", code)
	}
	return nil
}

func runElevatedOps(ops []string, hostsPath string) error {
	for len(ops) > 0 {
		switch {
		case ops[0] == "nrpt-add" && len(ops) >= 3:
			if err := runPowerShell(nrptRegisterScript(ops[1], ops[2])); err != nil {
				return err
			}
			ops = ops[3:]
		case ops[0] == "nrpt-remove" && len(ops) >= 2:
			if err := runPowerShell(nrptUnregisterScript(ops[1])); err != nil {
				return err
			}
			ops = ops[2:]
		case ops[0] == "hosts" && len(ops) >= 3:
			if err := installHosts(ops[1], ops[2], hostsPath); err != nil {
				return err
			}
			ops = ops[3:]
		default:
			return fmt.Errorf("unknown elevated operation %q", ops[0])
		}
	}
	return nil
}

// installHosts installs the staged hosts content only if its bytes hash to
// sha256Hex, reading them once so the check and the write see the same bytes,
// and keeps the previous file as hosts.hull-backup.
func installHosts(staged, sha256Hex, dest string) error {
	data, err := os.ReadFile(staged)
	if err != nil {
		return err
	}
	if sum := sha256.Sum256(data); !strings.EqualFold(hex.EncodeToString(sum[:]), sha256Hex) {
		return fmt.Errorf("the staged hosts file changed before it could be installed")
	}
	if old, err := os.ReadFile(dest); err == nil {
		if err := os.WriteFile(dest+".hull-backup", old, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(dest, data, 0o644)
}

// runPowerShell runs script as plain -Command text; a terminating error
// (the scripts set $ErrorActionPreference = 'Stop') makes it exit non-zero.
func runPowerShell(script string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	hideConsole(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("powershell: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// shellExecuteInfo is SHELLEXECUTEINFOW; x/sys/windows has ShellExecute but
// not ShellExecuteEx, which is needed to wait for the process.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.Handle
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIcon        windows.Handle
	hProcess     windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	seeMaskFlagNoUI       = 0x00000400 // no error dialog: it would block the console-less daemon
)

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellRunAs starts file elevated (the "runas" verb, one UAC prompt), with
// its window hidden, and returns its exit code.
func shellRunAs(file, params string) (uint32, error) {
	// ShellExecuteEx wants COM initialized on the calling thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil || err == syscall.Errno(1) { // S_FALSE: already initialized
		defer windows.CoUninitialize()
	}

	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI,
		lpVerb:       windows.StringToUTF16Ptr("runas"),
		lpFile:       windows.StringToUTF16Ptr(file),
		lpParameters: windows.StringToUTF16Ptr(params),
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		return 0, err
	}
	if info.hProcess == 0 {
		return 0, fmt.Errorf("no process handle")
	}
	defer func() { _ = windows.CloseHandle(info.hProcess) }()
	if _, err := windows.WaitForSingleObject(info.hProcess, windows.INFINITE); err != nil {
		return 0, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return 0, err
	}
	return code, nil
}
