//go:build windows

package config

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// MachineDir is the data directory of the ducktainer Windows service
// (installed by the MSI).
func MachineDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "ducktainer")
}

// machineDir picks the service's directory for the service itself
// (LocalSystem) and, once the service is installed, for an elevated
// administrator, the way root shares /var/lib/ducktainer on Linux.
func machineDir() string {
	t := windows.GetCurrentProcessToken()
	if u, err := t.GetTokenUser(); err == nil && u.User.Sid.IsWellKnown(windows.WinLocalSystemSid) {
		return MachineDir()
	}
	if t.IsElevated() {
		if _, err := os.Stat(filepath.Join(MachineDir(), "config.json")); err == nil {
			return MachineDir()
		}
	}
	return ""
}

// Restrict leaves dir accessible only to LocalSystem and Administrators
// (ProgramData is readable by every user by default).
func Restrict(dir string) error {
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
