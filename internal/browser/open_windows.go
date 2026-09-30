//go:build windows

package browser

import "os/exec"

// Open launches the default browser via the shell's URL association
// ("cmd /c start"), the standard way to do this on Windows without pulling
// in a COM/ShellExecute binding. The empty string after "start" is the
// window-title argument start.exe expects before a URL; omitting it would
// make start.exe treat a quoted URL as the title instead.
func Open(url string) error {
	return exec.Command("cmd", "/c", "start", "", url).Start()
}
