package tray

import (
	"github.com/bnulwh/ollama/app/tray/commontray"
	"github.com/bnulwh/ollama/app/tray/wintray"
)

func InitPlatformTray(icon, updateIcon []byte) (commontray.OllamaTray, error) {
	return wintray.InitTray(icon, updateIcon)
}
