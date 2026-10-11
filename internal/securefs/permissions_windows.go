//go:build windows

package securefs

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os/exec"
	"syscall"
)

// Go's chmod does not provide Windows ACL isolation. Limit new app directories
// to the current user; descendants inherit this ACL. No shell interpolation.
func Restrict(path string, directory bool) error {
	user, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return e
	}
	grant := "*" + user.User.Sid.String() + ":"
	if directory {
		grant += "(OI)(CI)"
	}
	grant += "F"
	cmd := exec.Command("icacls.exe", path, "/inheritance:r", "/grant:r", grant)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if e = cmd.Run(); e != nil {
		return fmt.Errorf("restrict application ACL: %w", e)
	}
	return nil
}
