package ticketqr

import (
	"errors"
	"strings"

	goqr "github.com/piglig/go-qr"
)

const (
	moduleScale = 1
	quietZone   = 4
)

var errInvalidCredential = errors.New("QR credential is invalid")

// RenderSVG encodes the complete authoritative qr1 credential into a QR image.
// The generated SVG contains only library-produced geometry and fixed colors.
func RenderSVG(credential string) ([]byte, error) {
	code, err := encode(credential)
	if err != nil {
		return nil, err
	}

	return code.ToSVGBytes(
		goqr.NewQrCodeImgConfig(moduleScale, quietZone),
	)
}

func encode(credential string) (*goqr.QrCode, error) {
	if credential != strings.TrimSpace(credential) ||
		!strings.HasPrefix(credential, "qr1.") {
		return nil, errInvalidCredential
	}

	return goqr.EncodeText(credential, goqr.Medium)
}
