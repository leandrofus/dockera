//go:build windows

package auth

import (
	"ducktainer/internal/i18n"
	"errors"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Available reports whether system-user authentication was compiled in:
// on Windows, LogonUser is always there.
const Available = true

var (
	advapi32      = windows.NewLazySystemDLL("advapi32.dll")
	procLogonUser = advapi32.NewProc("LogonUserW")

	netapi32                    = windows.NewLazySystemDLL("netapi32.dll")
	procNetLocalGroupGetMembers = netapi32.NewProc("NetLocalGroupGetMembers")
)

const (
	logon32LogonNetwork    = 3
	logon32ProviderDefault = 0
	maxPreferredLength     = 0xFFFFFFFF
)

// Authenticate checks a password with LogonUser, so local accounts, domain
// accounts (DOMAIN\user or user@domain) and the account policies (disabled,
// locked out, expired) all apply. A network logon needs no privilege, so it
// works for the service (LocalSystem) and for a user running ducktainer.
// service and remoteHost are PAM concepts with no Windows counterpart.
func Authenticate(service, username, password, remoteHost string) error {
	if username == "" || password == "" {
		return ErrBadCredentials
	}
	var domain *uint16
	name := strings.TrimPrefix(username, `.\`)
	if d, u, ok := strings.Cut(name, `\`); ok {
		domain, name = windows.StringToUTF16Ptr(d), u
	} else if !strings.Contains(name, "@") {
		domain = windows.StringToUTF16Ptr(".") // a bare name is a local account
	}
	u16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return ErrBadCredentials
	}
	p16, err := windows.UTF16PtrFromString(password)
	if err != nil {
		return ErrBadCredentials
	}
	var token windows.Token
	r, _, e := procLogonUser.Call(uintptr(unsafe.Pointer(u16)), uintptr(unsafe.Pointer(domain)), uintptr(unsafe.Pointer(p16)),
		logon32LogonNetwork, logon32ProviderDefault, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		var errno syscall.Errno
		if errors.As(e, &errno) && errno == windows.ERROR_LOGON_FAILURE {
			return ErrBadCredentials
		}
		return i18n.Errorf("account not valid: %v", e)
	}
	token.Close()
	return nil
}

// DefaultAdminGroups are the groups whose members administer ducktainer on a
// fresh install: the local Administrators (named in the system's language),
// Docker Desktop's docker-users and ducktainer-admins.
func DefaultAdminGroups() []string {
	admins := "Administrators"
	if sid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid); err == nil {
		if name, _, _, err := sid.LookupAccount(""); err == nil {
			admins = name
		}
	}
	return []string{admins, "docker-users", "ducktainer-admins"}
}

// localGroupMembersInfo3 is LOCALGROUP_MEMBERS_INFO_3.
type localGroupMembersInfo3 struct {
	DomainAndName *uint16
}

// Group returns a local group's members (as DOMAIN\name). Domain groups
// exist but can't be listed from here.
func Group(name string) GroupInfo {
	g := GroupInfo{Name: name, Members: []string{}}
	sid, _, kind, err := windows.LookupSID("", name)
	if err != nil || (kind != windows.SidTypeAlias && kind != windows.SidTypeGroup && kind != windows.SidTypeWellKnownGroup) {
		return g
	}
	g.Exists, g.GID = true, sid.String()
	n16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return g
	}
	var buf *byte
	var read, total uint32
	var resume uintptr
	r, _, _ := procNetLocalGroupGetMembers.Call(0, uintptr(unsafe.Pointer(n16)), 3, uintptr(unsafe.Pointer(&buf)),
		maxPreferredLength, uintptr(unsafe.Pointer(&read)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&resume)))
	if r != 0 || buf == nil {
		return g // not a local group (e.g. a domain group)
	}
	defer windows.NetApiBufferFree(buf)
	for _, m := range unsafe.Slice((*localGroupMembersInfo3)(unsafe.Pointer(buf)), read) {
		if m.DomainAndName != nil {
			g.Members = append(g.Members, windows.UTF16PtrToString(m.DomainAndName))
		}
	}
	sort.Strings(g.Members)
	return g
}
