//go:build !windows

package browser

import (
	"os/exec"
	"runtime"
)

// Open is a best-effort non-Windows implementation (the agent only ships
// for Windows; this exists so `go build`/`go vet`/`go test` run in the
// Linux build container and so a developer can smoke-test the status page
// locally on macOS).
func Open(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, url).Start()
}
