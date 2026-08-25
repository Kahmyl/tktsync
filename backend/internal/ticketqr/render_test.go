package ticketqr

import (
	"bytes"
	"testing"

	goqr "github.com/piglig/go-qr"
)

const testCredential = "qr1.1.123e4567-e89b-12d3-a456-426614174000.c29tZS1hdXRob3JpdGF0aXZlLW1hYy1ieXRlcw"

func TestRenderSVGDeterministicallyEncodesExactCredential(t *testing.T) {
	first, err := RenderSVG(testCredential)
	if err != nil {
		t.Fatalf("render first SVG: %v", err)
	}
	second, err := RenderSVG(testCredential)
	if err != nil {
		t.Fatalf("render second SVG: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatal("QR SVG output is not deterministic")
	}
	if !bytes.HasPrefix(first, []byte(`<svg xmlns="http://www.w3.org/2000/svg"`)) {
		t.Fatalf("unexpected SVG prefix: %q", first[:min(80, len(first))])
	}
	if bytes.Contains(first, []byte(testCredential)) ||
		bytes.Contains(first, []byte("qr1.")) {
		t.Fatal("raw credential leaked into SVG markup")
	}

	code, err := encode(testCredential)
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	image, err := code.ToImage(goqr.NewQrCodeImgConfig(8, quietZone))
	if err != nil {
		t.Fatalf("render decodable image: %v", err)
	}
	decoded, err := goqr.Decode(image)
	if err != nil {
		t.Fatalf("decode generated QR: %v", err)
	}
	if decoded != testCredential {
		t.Fatalf("decoded QR=%q want exact credential %q", decoded, testCredential)
	}
}

func TestRenderSVGRejectsNonAuthoritativePayload(t *testing.T) {
	for _, payload := range []string{"", "ticket-id", " qr1.example", "https://example.test/qr"} {
		if _, err := RenderSVG(payload); err == nil {
			t.Fatalf("RenderSVG(%q) succeeded", payload)
		}
	}
}
