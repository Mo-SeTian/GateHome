package imageasset

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestImageContentAndSVGValidation(t *testing.T) {
	var data bytes.Buffer
	png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	if !Validate("icon.png", data.Bytes()) || Validate("icon.jpg", data.Bytes()) || Validate("icon.png", []byte(`<html><script>alert(1)</script></html>`)) {
		t.Fatal("image content/extension validation failed")
	}
	safe := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><defs><path id="x" d="M0 0"/></defs><use href="#x" fill="currentColor"/></svg>`)
	if !Validate("icon.svg", safe) {
		t.Fatal("safe SVG rejected")
	}
	for _, unsafe := range []string{
		`<svg><script>alert(1)</script></svg>`,
		`<svg onload="alert(1)"></svg>`,
		`<svg><foreignObject><div>Active HTML</div></foreignObject></svg>`,
		`<svg><use href="https://example.test/icon.svg#x"/></svg>`,
		`<svg><style>@import url(https://example.test/);</style></svg>`,
		`<!DOCTYPE svg [<!ENTITY e SYSTEM "file:///etc/passwd">]><svg>&e;</svg>`,
		`<svg></svg><svg></svg>`,
		`<svg><path fill="url(https://example.test/)"/></svg>`,
	} {
		if Validate("icon.svg", []byte(unsafe)) {
			t.Fatal("active or external SVG accepted")
		}
	}
	if _, err := Extension(make([]byte, Limit+1)); err == nil {
		t.Fatal("oversized image accepted")
	}
}
