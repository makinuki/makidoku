//go:build tray

package tray

import (
	"context"
	_ "embed"
	"log"
	"os/exec"
	"runtime"

	"fyne.io/systray"
)

//go:embed icon.ico
var iconData []byte

// Run starts the system tray and blocks until the tray is quit or the context
// is cancelled. It must be called from the main goroutine on some platforms.
func Run(ctx context.Context, addr string) {
	go func() {
		<-ctx.Done()
		systray.Quit()
	}()

	systray.Run(func() { onReady(addr) }, func() {})
}

func onReady(addr string) {
	systray.SetIcon(iconData)
	systray.SetTitle("MakiDoku")
	systray.SetTooltip("MakiDoku")

	mOpen := systray.AddMenuItem("Open Web UI", "Open MakiDoku in browser")
	mQuit := systray.AddMenuItem("Exit", "Quit MakiDoku")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				openBrowser("http://" + addr)
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("tray: open browser: %v", err)
	}
}
