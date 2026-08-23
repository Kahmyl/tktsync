package webhook

import (
	"encoding/base64"
	"net"
	"testing"
)

func TestProductionDestinationValidationRejectsSSRFAddresses(
	t *testing.T,
) {
	values := []string{
		"http://example.com/hook",
		"https://127.0.0.1/hook",
		"https://169.254.169.254/latest",
		"https://100.64.0.1/hook",
		"https://192.0.2.1/hook",
		"https://198.18.0.1/hook",
		"https://198.51.100.1/hook",
		"https://203.0.113.1/hook",
		"https://240.0.0.1/hook",
		"https://[2001:db8::1]/hook",
		"https://user:pass@example.com/hook",
	}

	for _, raw := range values {
		if _, err :=
			validateDestination(
				raw,
				false,
			); err == nil {
			t.Fatalf(
				"destination %q unexpectedly accepted",
				raw,
			)
		}
	}

	got, err :=
		validateDestination(
			"https://hooks.example.com/tktsync",
			false,
		)
	if err != nil ||
		got == "" {
		t.Fatalf(
			"public HTTPS rejected: %v",
			err,
		)
	}
}

func TestUnsafeIPRejectsSpecialUseNetworks(
	t *testing.T,
) {
	blocked := []string{
		"0.1.2.3",
		"10.0.0.1",
		"100.64.0.1",
		"127.0.0.1",
		"169.254.1.1",
		"172.16.0.1",
		"192.0.0.1",
		"192.0.2.1",
		"192.168.0.1",
		"198.18.0.1",
		"198.51.100.1",
		"203.0.113.1",
		"224.0.0.1",
		"240.0.0.1",
		"::1",
		"fc00::1",
		"fe80::1",
		"ff02::1",
		"2001:db8::1",
	}

	for _, raw := range blocked {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf(
				"invalid test IP %q",
				raw,
			)
		}

		if !unsafeIP(ip) {
			t.Fatalf(
				"special-use IP %q unexpectedly allowed",
				raw,
			)
		}
	}

	allowed := []string{
		"1.1.1.1",
		"8.8.8.8",
		"2606:4700:4700::1111",
	}

	for _, raw := range allowed {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf(
				"invalid test IP %q",
				raw,
			)
		}

		if unsafeIP(ip) {
			t.Fatalf(
				"public IP %q unexpectedly blocked",
				raw,
			)
		}
	}
}

func TestVersionedSecretBoxReadsHistoricalKeys(
	t *testing.T,
) {
	oldRaw := make([]byte, 32)
	newRaw := make([]byte, 32)

	for index := range oldRaw {
		oldRaw[index] =
			byte(index + 1)
		newRaw[index] =
			byte(200 - index)
	}

	oldEncoded :=
		base64.RawURLEncoding.EncodeToString(
			oldRaw,
		)

	newEncoded :=
		base64.RawURLEncoding.EncodeToString(
			newRaw,
		)

	oldBox, err :=
		NewSecretBox(
			oldEncoded,
		)
	if err != nil {
		t.Fatal(err)
	}

	oldCiphertext, err :=
		oldBox.Seal(
			[]byte("historical-secret"),
		)
	if err != nil {
		t.Fatal(err)
	}

	ring, err :=
		NewVersionedSecretBox(
			2,
			newEncoded,
			`{"1":"`+
				oldEncoded+
				`"}`,
		)
	if err != nil {
		t.Fatal(err)
	}

	opened, err :=
		ring.OpenVersion(
			1,
			oldCiphertext,
		)
	if err != nil {
		t.Fatal(err)
	}

	if string(opened) !=
		"historical-secret" {
		t.Fatalf(
			"historical plaintext=%q",
			opened,
		)
	}

	newCiphertext, err :=
		ring.SealVersion(
			2,
			[]byte("current-secret"),
		)
	if err != nil {
		t.Fatal(err)
	}

	newBox, err :=
		NewSecretBox(
			newEncoded,
		)
	if err != nil {
		t.Fatal(err)
	}

	opened, err =
		newBox.Open(
			newCiphertext,
		)
	if err != nil {
		t.Fatal(err)
	}

	if string(opened) !=
		"current-secret" {
		t.Fatalf(
			"current plaintext=%q",
			opened,
		)
	}

	if _, err =
		ring.OpenVersion(
			99,
			oldCiphertext,
		); err == nil {
		t.Fatal(
			"unknown encryption key version was accepted",
		)
	}
}
