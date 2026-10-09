package gateway

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testZIP(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, b := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testELF(arch string) []byte {
	b := make([]byte, 64)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(b[16:], 2)
	machine := uint16(62)
	if arch == "arm64" {
		machine = 183
	}
	binary.LittleEndian.PutUint16(b[18:], machine)
	binary.LittleEndian.PutUint32(b[20:], 1)
	binary.LittleEndian.PutUint16(b[52:], 64)
	return b
}

func testRelease(t *testing.T, version, arch string) []byte {
	b := testELF(arch)
	sum := sha256.Sum256(b)
	name := "dist/gatehouse-linux-" + arch
	m := releaseManifest{Format: "gatehouse-update-v1", Version: version, Files: map[string]releaseFile{name: {SHA256: hex.EncodeToString(sum[:]), Size: int64(len(b))}}}
	meta, _ := json.Marshal(m)
	return testZIP(t, map[string][]byte{"manifest.json": meta, name: b})
}

func TestUpdatePackageValidationAndVersions(t *testing.T) {
	version, b, err := inspectRelease(testRelease(t, "0.0.2", "amd64"), "amd64")
	if err != nil || version != "0.0.2" || len(b) == 0 {
		t.Fatal("valid update package rejected")
	}
	for _, data := range [][]byte{[]byte("not zip"), testZIP(t, map[string][]byte{"../escape": []byte("unsafe")}), testZIP(t, map[string][]byte{"/absolute": nil}), testZIP(t, map[string][]byte{"a\\b": nil}), testRelease(t, "0.0.2", "arm64")} {
		if _, _, err := inspectRelease(data, "amd64"); err == nil {
			t.Fatal("unsafe or wrong-architecture update accepted")
		}
	}
	files, err := readZIP(testRelease(t, "0.0.2", "amd64"), 512<<20, 1000)
	if err != nil {
		t.Fatal(err)
	}
	files["dist/gatehouse-linux-amd64"][20] ^= 1
	if _, _, err := inspectRelease(testZIP(t, files), "amd64"); err == nil {
		t.Fatal("altered binary checksum accepted")
	}
	if _, err := compareVersions("../0.0.2", Version); err == nil {
		t.Fatal("invalid version accepted")
	}
	cmp, err := compareVersions("0.10.0", "0.2.9")
	if err != nil || cmp <= 0 {
		t.Fatal("versions compared lexically")
	}
}

func TestEncryptedBackupRoundTripAndTamper(t *testing.T) {
	a, _ := testAdmin(t)
	state := a.store.Snapshot()
	state.ProxyPassword = "TEST_ONLY_PROXY_SECRET"
	payload := backupPayload{State: state, Certificates: map[string][]byte{"staging-nas.example.com.json": []byte(`{"test":"TEST_ONLY_PRIVATE_KEY"}`)}}
	data, err := encodeBackup(payload, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{state.CloudflareToken, state.PasswordHash, state.ProxyPassword, "TEST_ONLY_PRIVATE_KEY", "TEST_ONLY_BACKUP_PASSWORD"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("backup exposed a secret")
		}
	}
	decoded, _, err := decodeBackup(data, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || decoded.State.CloudflareToken != state.CloudflareToken || decoded.State.ProxyPassword != state.ProxyPassword || len(decoded.Certificates) != 1 {
		t.Fatal("backup did not restore protected configuration")
	}
	if _, _, err := decodeBackup(data, "TEST_ONLY_WRONG_PASSWORD"); err == nil {
		t.Fatal("wrong password accepted")
	}
	files, err := readZIP(data, maxBackupBytes+1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	files["backup.enc"][0] ^= 1
	if _, _, err := decodeBackup(testZIP(t, files), "TEST_ONLY_BACKUP_PASSWORD"); err == nil {
		t.Fatal("tampered backup accepted")
	}
	dir := t.TempDir()
	if err := restorePayload(dir, decoded); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir)
	if err != nil || store.Snapshot().ProxyPassword != state.ProxyPassword {
		t.Fatal("configuration restore failed")
	}
	info, err := os.Stat(filepath.Join(dir, "state.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("restored credentials must be private")
	}
}

func TestMaintenanceRestoreRollbackPreservesUnknownFields(t *testing.T) {
	a, _ := testAdmin(t)
	dir := filepath.Dir(a.store.path)
	app := t.TempDir()
	oldProgram := []byte("TEST_ONLY_OLD_PROGRAM")
	if err := os.WriteFile(filepath.Join(app, "gatehouse"), oldProgram, 0750); err != nil {
		t.Fatal(err)
	}
	fields := map[string]json.RawMessage{}
	data, _ := os.ReadFile(a.store.path)
	json.Unmarshal(data, &fields)
	fields["future_setting"] = json.RawMessage(`{"keep":true}`)
	raw, _ := json.Marshal(fields)
	atomicWrite(a.store.path, raw)
	m, err := NewMaintenance(dir, app)
	if err != nil {
		t.Fatal(err)
	}
	changed := backupPayload{State: a.store.Snapshot(), Certificates: map[string][]byte{}}
	changed.State.Config.Zone = "restored.example.com"
	writeJSON(filepath.Join(m.dir, "restore-staged.json"), changed)
	writeJSON(filepath.Join(m.dir, "operation.json"), maintenanceOperation{Kind: "restore", Version: Version})
	transition, err := m.beginTransition()
	if err != nil || !transition {
		t.Fatal("restore transition failed")
	}
	if err := m.rollbackTransition(); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(a.store.path)
	if !bytes.Contains(restored, []byte("future_setting")) || bytes.Contains(restored, []byte("restored.example.com")) {
		t.Fatal("rollback lost prior state or unknown fields")
	}
	program, _ := os.ReadFile(filepath.Join(app, "gatehouse"))
	if !bytes.Equal(program, oldProgram) {
		t.Fatal("program rollback failed")
	}
}

func TestSupervisorStartupFailureRollsBack(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("Unix process test")
	}
	a, _ := testAdmin(t)
	dir := filepath.Dir(a.store.path)
	app := t.TempDir()
	// This local test child writes the same readiness marker as the real service.
	script := "#!/bin/sh\ndata=\"$2\"\nprintf '{\"pid\":%s,\"version\":\"" + Version + "\"}' \"$$\" > \"$data/maintenance/ready.json\"\ntrap 'exit 0' TERM INT\nwhile :; do sleep 0.1; done\n"
	if err := os.WriteFile(filepath.Join(app, "gatehouse"), []byte(script), 0750); err != nil {
		t.Fatal(err)
	}
	m, err := NewMaintenance(dir, app)
	if err != nil {
		t.Fatal(err)
	}
	binary := testELF(releaseArch())
	sum := sha256.Sum256(binary)
	atomicWrite(filepath.Join(m.dir, "update-staged"), binary)
	writeJSON(filepath.Join(m.dir, "operation.json"), maintenanceOperation{Kind: "update", Version: "0.0.2", Digest: hex.EncodeToString(sum[:])})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Supervise(ctx, dir, app, "127.0.0.1:16666") }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(filepath.Join(app, "gatehouse"))
		ready, _ := os.ReadFile(filepath.Join(m.dir, "ready.json"))
		if bytes.Equal(b, []byte(script)) && len(ready) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup failure did not roll back")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not stop")
	}
}

func TestMaintenanceAPIBackupAuthAndUploadCSRF(t *testing.T) {
	a, _ := testAdmin(t)
	m, err := NewMaintenance(filepath.Dir(a.store.path), "")
	if err != nil {
		t.Fatal(err)
	}
	a.SetMaintenance(m)
	h := a.Handler()
	cookie := loginForTest(t, h)
	if w := adminRequest(h, "POST", "/api/maintenance/backup", map[string]string{"password": "TEST_ONLY_BACKUP_PASSWORD"}, nil, ""); w.Code != 401 {
		t.Fatal("unauthenticated backup access")
	}
	password := strings.Repeat("TEST_ONLY_", 1024)
	w := adminRequest(h, "POST", "/api/maintenance/backup", map[string]string{"password": password}, cookie, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatal("backup download failed")
	}
	for _, header := range []bool{false, true} {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		file, _ := writer.CreateFormFile("file", "backup.zip")
		io.Copy(file, bytes.NewReader(w.Body.Bytes()))
		writer.WriteField("password", password)
		writer.Close()
		r := httptest.NewRequest("POST", "http://localhost:16666/api/maintenance/inspect-backup", &buf)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		r.AddCookie(cookie)
		if header {
			r.Header.Set("X-Gatehouse-Request", "1")
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		if !header && out.Code != 403 {
			t.Fatal("upload without CSRF header accepted")
		}
		if header && (out.Code != 200 || strings.Contains(out.Body.String(), "TEST_ONLY_FAKE_TOKEN")) {
			t.Fatal("backup preview failed or exposed credential")
		}
	}
}

func TestRestartUsesCompatibleUpdateProtocol(t *testing.T) {
	t.Setenv("GATEHOUSE_SUPERVISED", "1")
	t.Setenv("GATEHOUSE_SUNPANEL_STORAGE", "1")
	a, _ := testAdmin(t)
	dir := filepath.Dir(a.store.path)
	app := t.TempDir()
	program := testELF(releaseArch())
	if err := os.WriteFile(filepath.Join(app, "gatehouse"), program, 0750); err != nil {
		t.Fatal(err)
	}
	m, err := NewMaintenance(dir, app)
	if err != nil {
		t.Fatal(err)
	}
	a.SetMaintenance(m)
	h := a.Handler()
	if w := adminRequest(h, "POST", "/api/maintenance/restart", map[string]any{}, nil, ""); w.Code != 401 {
		t.Fatal("unauthenticated restart accepted")
	}
	cookie := loginForTest(t, h)
	if w := adminRequest(h, "POST", "/api/maintenance/restart", map[string]any{}, cookie, "https://evil.example"); w.Code != 403 {
		t.Fatal("cross-origin restart accepted")
	}
	w := adminRequest(h, "POST", "/api/maintenance/restart", map[string]any{}, cookie, "")
	// Restart reapplies the same binary using the protocol implemented by 0.0.1 launchers.
	sum := sha256.Sum256(program)
	want := maintenanceOperation{Kind: "update", Version: Version, Digest: hex.EncodeToString(sum[:])}
	if m.Available() {
		if w.Code != 202 || !m.Busy() {
			t.Fatal("managed restart was not scheduled")
		}
		select {
		case <-m.RestartSignal():
		default:
			t.Fatal("restart signal missing")
		}
		data, err := os.ReadFile(filepath.Join(m.dir, "operation.json"))
		var got maintenanceOperation
		if err != nil || json.Unmarshal(data, &got) != nil || got != want {
			t.Fatal("restart did not use the compatible protocol")
		}
		if err := m.scheduleRestart(); err == nil {
			t.Fatal("duplicate restart accepted while busy")
		}
	} else {
		if w.Code != 409 || m.Busy() {
			t.Fatal("unsupported deployment scheduled restart")
		}
		// Exercise the launcher's transaction on non-Linux test hosts too.
		atomicWrite(filepath.Join(m.dir, "update-staged"), program)
		writeJSON(filepath.Join(m.dir, "operation.json"), want)
	}
	before, _ := os.ReadFile(a.store.path)
	changed, err := m.beginTransition()
	if err != nil || !changed {
		t.Fatal("restart transition rejected")
	}
	after, _ := os.ReadFile(a.store.path)
	if !bytes.Equal(before, after) {
		t.Fatal("restart changed configuration")
	}
	if err := m.finishTransition(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.dir, "operation.json")); !os.IsNotExist(err) {
		t.Fatal("restart request not cleared")
	}
}

func TestRestartAllowsLegacyLauncherWithoutSunPanelRollback(t *testing.T) {
	t.Setenv("GATEHOUSE_SUPERVISED", "1")
	t.Setenv("GATEHOUSE_SUNPANEL_STORAGE", "0")
	a, _ := testAdmin(t)
	app := t.TempDir()
	program := testELF(releaseArch())
	if err := os.WriteFile(filepath.Join(app, "gatehouse"), program, 0750); err != nil {
		t.Fatal(err)
	}
	m, err := NewMaintenance(filepath.Dir(a.store.path), app)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Available() {
		t.Skip("restart maintenance is only available on supervised Linux amd64/arm64 deployments")
	}
	if m.SunPanelFilesSupported() {
		t.Fatal("legacy launcher unexpectedly reported Sun-Panel rollback support")
	}
	if err := m.scheduleRestart(); err != nil {
		t.Fatalf("legacy launcher should still allow a plain restart: %v", err)
	}
}

func TestRestartChecksNewPortsBeforeStoppingService(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	current, next := Config{}, DefaultConfig()
	next.Groups[0].HTTPPort = port
	next.Groups[0].HTTPSPort = 0
	if err := checkRestartPorts(current, next); err == nil || !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Fatal("occupied new port did not prevent restart")
	}
	next.Groups[0].Enabled = false
	if err := checkRestartPorts(current, next); err != nil {
		t.Fatal("disabled listener was checked")
	}
	next.Groups[0].Enabled = true
	current = DefaultConfig()
	current.Groups[0].HTTPPort = port
	if err := checkRestartPorts(current, next); err != nil {
		t.Fatal("currently owned port prevented restart")
	}
	current = Config{}
	listener.Close()
	if err := checkRestartPorts(current, next); err != nil {
		t.Fatal("available new port prevented restart")
	}
}
