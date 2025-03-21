//go:build !windows

package tray

import (
	"errors"

	"github.com/bnulwh/ollama/app/tray/commontray"
)

func InitPlatformTray(icon, updateIcon []byte) (commontray.OllamaTray, error) {
	return nil, errors.New("not implemented")
}
