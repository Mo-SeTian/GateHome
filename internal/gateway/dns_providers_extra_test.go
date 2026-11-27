package gateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func verifyAliSignature(t *testing.T, form url.Values, secret string) {
	t.Helper()
	gotSig := form.Get("Signature")
	if gotSig == "" || form.Get("SignatureMethod") != "HMAC-SHA1" || form.Get("SignatureVersion") != "1.0" {
		t.Fatalf("missing ali signature params: %v", form)
	}
	keys := make([]string, 0, len(form))
	for k := range form {
		if k == "Signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, popPercentEncode(k)+"="+popPercentEncode(form.Get(k)))
	}
	canonicalQuery := strings.Join(parts, "&")
	stringToSign := "POST&%2F&" + popPercentEncode(canonicalQuery)
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	expectedSig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if gotSig != expectedSig {
		t.Fatalf("ali signature mismatch: got %q want %q", gotSig, expectedSig)
	}
}

func TestAliDNSProviderSignSyncAndACME(t *testing.T) {
	var actions []string
	var records = []aliRecord{}
	nextID := 100

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ := io.ReadAll(r.Body)
		form, err := url.ParseQuery(string(rawBody))
		if err != nil {
			t.Fatalf("parse ali body failed: %v", err)
		}
		verifyAliSignature(t, form, "TEST_ALI_SECRET")
		action := form.Get("Action")
		actions = append(actions, action)
		switch action {
		case "DescribeDomainInfo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"DomainId":   "dom-1",
				"DomainName": form.Get("DomainName"),
			})
		case "DescribeDomainRecords":
			rr := form.Get("RRKeyWord")
			tp := form.Get("Type")
			var matched []aliRecord
			for _, rec := range records {
				if rec.RR == rr && (tp == "" || rec.Type == tp) {
					matched = append(matched, rec)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"TotalCount":    len(matched),
				"DomainRecords": map[string]any{"Record": matched},
			})
		case "AddDomainRecord":
			nextID++
			id := "rec-" + strconv.Itoa(nextID)
			records = append(records, aliRecord{
				RecordID: id,
				RR:       form.Get("RR"),
				Type:     form.Get("Type"),
				Value:    form.Get("Value"),
				TTL:      600,
			})
			_ = json.NewEncoder(w).Encode(map[string]any{"RecordId": id})
		case "UpdateDomainRecord":
			id := form.Get("RecordId")
			for i := range records {
				if records[i].RecordID == id {
					records[i].Value = form.Get("Value")
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"RecordId": id})
		case "DeleteDomainRecord":
			id := form.Get("RecordId")
			records = slices.DeleteFunc(records, func(rec aliRecord) bool { return rec.RecordID == id })
			_ = json.NewEncoder(w).Encode(map[string]any{"RecordId": id})
		default:
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"Code": "UnsupportedAction"})
		}
	}))
	defer srv.Close()

	client := NewAliDNS("TEST_ALI_KEY_ID", "TEST_ALI_SECRET")
	client.BaseURL = srv.URL
	client.Client = srv.Client()

	zoneID, err := client.ZoneID(context.Background(), "example.com")
	if err != nil || zoneID != "example.com" {
		t.Fatalf("ZoneID failed: zone=%q err=%v", zoneID, err)
	}
	if _, err := client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "nas.example.com", Type: "A"}, netip.MustParseAddr("192.168.1.1")); err == nil {
		t.Fatal("private IP should be rejected")
	}
	changed, err := client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "nas.example.com", Type: "A"}, netip.MustParseAddr("1.2.3.4"))
	if err != nil || !changed || len(records) != 1 || records[0].Value != "1.2.3.4" {
		t.Fatalf("create failed: changed=%v err=%v records=%+v", changed, err, records)
	}
	changed, err = client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "nas.example.com", Type: "A"}, netip.MustParseAddr("1.2.3.4"))
	if err != nil || changed {
		t.Fatalf("unchanged record should no-op: changed=%v err=%v", changed, err)
	}
	changed, err = client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "nas.example.com", Type: "A"}, netip.MustParseAddr("5.6.7.8"))
	if err != nil || !changed || records[0].Value != "5.6.7.8" {
		t.Fatalf("update failed: changed=%v err=%v records=%+v", changed, err, records)
	}

	// CNAME conflict check
	records = append(records, aliRecord{RecordID: "cname-1", RR: "cname", Type: "CNAME", Value: "other.example.net"})
	if _, err := client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "cname.example.com", Type: "A"}, netip.MustParseAddr("1.2.3.4")); err == nil {
		t.Fatal("CNAME conflict should fail")
	}

	// ACME TXT Present & CleanUp via adapter
	adapter := newDNS01ChallengeAdapter(client)
	adapter.findZone = func(string) (string, error) { return "example.com.", nil }
	if err := adapter.Present("*.example.com", "tok", "key-auth-1"); err != nil {
		t.Fatalf("ACME Present failed: %v", err)
	}
	hasTXT := false
	for _, r := range records {
		if r.Type == "TXT" && r.RR == "_acme-challenge" {
			hasTXT = true
		}
	}
	if !hasTXT {
		t.Fatalf("TXT record not added: %+v", records)
	}
	if err := adapter.CleanUp("*.example.com", "tok", "key-auth-1"); err != nil {
		t.Fatalf("ACME CleanUp failed: %v", err)
	}
	for _, r := range records {
		if r.Type == "TXT" {
			t.Fatalf("TXT record not cleaned up: %+v", records)
		}
	}
	if !slices.Contains(actions, "UpdateDomainRecord") || !slices.Contains(actions, "DeleteDomainRecord") {
		t.Fatalf("unexpected actions: %v", actions)
	}
}

func TestTencentCloudDNSProviderSignSyncAndACME(t *testing.T) {
	var records []tencentRecord
	nextID := uint64(200)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ := io.ReadAll(r.Body)
		action := r.Header.Get("X-TC-Action")
		ts := r.Header.Get("X-TC-Timestamp")
		auth := r.Header.Get("Authorization")
		if r.Header.Get("X-TC-Version") != "2021-03-23" || ts == "" || !strings.HasPrefix(auth, "TC3-HMAC-SHA256 ") {
			t.Fatalf("missing TC3 headers: %+v", r.Header)
		}
		secInt, _ := strconv.ParseInt(ts, 10, 64)
		date := time.Unix(secInt, 0).UTC().Format("2006-01-02")
		canonHeaders := "content-type:application/json; charset=utf-8\nhost:" + r.Host + "\nx-tc-action:" + strings.ToLower(action) + "\n"
		canonReq := "POST\n/\n\n" + canonHeaders + "\ncontent-type;host;x-tc-action\n" + sha256Hex(rawBody)
		scope := date + "/dnspod/tc3_request"
		stringToSign := "TC3-HMAC-SHA256\n" + ts + "\n" + scope + "\n" + sha256Hex([]byte(canonReq))
		secretDate := hmacSHA256Bytes([]byte("TC3TEST_TC_SECRET_KEY"), date)
		secretService := hmacSHA256Bytes(secretDate, "dnspod")
		secretSigning := hmacSHA256Bytes(secretService, "tc3_request")
		expectedSig := hex.EncodeToString(hmacSHA256Bytes(secretSigning, stringToSign))
		expectedAuth := "TC3-HMAC-SHA256 Credential=TEST_TC_SECRET_ID/" + scope + ", SignedHeaders=content-type;host;x-tc-action, Signature=" + expectedSig
		if auth != expectedAuth {
			t.Fatalf("TC3 auth mismatch:\ngot:  %s\nwant: %s", auth, expectedAuth)
		}

		var body map[string]any
		_ = json.Unmarshal(rawBody, &body)
		switch action {
		case "DescribeDomain":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Response": map[string]any{"DomainInfo": map[string]any{"Domain": body["Domain"]}},
			})
		case "DescribeRecordList":
			sub, _ := body["Subdomain"].(string)
			var matched []tencentRecord
			for _, rec := range records {
				if rec.Name == sub {
					matched = append(matched, rec)
				}
			}
			if len(matched) == 0 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Response": map[string]any{
						"Error": map[string]any{"Code": "ResourceNotFound.NoDataOfRecord", "Message": "no records"},
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Response": map[string]any{
					"RecordCountInfo": map[string]any{"TotalCount": len(matched)},
					"RecordList":      matched,
				},
			})
		case "CreateRecord":
			nextID++
			records = append(records, tencentRecord{
				RecordID: nextID,
				Name:     body["SubDomain"].(string),
				Type:     body["RecordType"].(string),
				Value:    body["Value"].(string),
				Line:     body["RecordLine"].(string),
				TTL:      600,
			})
			_ = json.NewEncoder(w).Encode(map[string]any{"Response": map[string]any{"RecordId": nextID}})
		case "ModifyRecord":
			id := uint64(body["RecordId"].(float64))
			for i := range records {
				if records[i].RecordID == id {
					records[i].Value = body["Value"].(string)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Response": map[string]any{"RecordId": id}})
		case "DeleteRecord":
			id := uint64(body["RecordId"].(float64))
			records = slices.DeleteFunc(records, func(rec tencentRecord) bool { return rec.RecordID == id })
			_ = json.NewEncoder(w).Encode(map[string]any{"Response": map[string]any{}})
		default:
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()

	client := NewTencentCloudDNS("TEST_TC_SECRET_ID", "TEST_TC_SECRET_KEY")
	client.BaseURL = srv.URL
	client.Client = srv.Client()

	zoneID, err := client.ZoneID(context.Background(), "example.com")
	if err != nil || zoneID != "example.com" {
		t.Fatalf("ZoneID failed: %v", err)
	}
	// Create after ResourceNotFound.NoDataOfRecord
	changed, err := client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "example.com", Type: "A"}, netip.MustParseAddr("1.2.3.4"))
	if err != nil || !changed || len(records) != 1 || records[0].Name != "@" {
		t.Fatalf("create failed: changed=%v err=%v records=%+v", changed, err, records)
	}
	changed, err = client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "example.com", Type: "A"}, netip.MustParseAddr("1.2.3.4"))
	if err != nil || changed {
		t.Fatalf("unchanged failed: changed=%v err=%v", changed, err)
	}
	changed, err = client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "example.com", Type: "A"}, netip.MustParseAddr("9.9.9.9"))
	if err != nil || !changed || records[0].Value != "9.9.9.9" {
		t.Fatalf("modify failed: changed=%v err=%v records=%+v", changed, err, records)
	}

	adapter := newDNS01ChallengeAdapter(client)
	adapter.findZone = func(string) (string, error) { return "example.com.", nil }
	if err := adapter.Present("sub.example.com", "tok", "tc-key-auth"); err != nil {
		t.Fatalf("Present failed: %v", err)
	}
	if err := adapter.CleanUp("sub.example.com", "tok", "tc-key-auth"); err != nil {
		t.Fatalf("CleanUp failed: %v", err)
	}
	for _, r := range records {
		if r.Type == "TXT" {
			t.Fatalf("TXT record left behind: %+v", records)
		}
	}
}

func TestHuaweiCloudDNSProviderSignSyncAndACME(t *testing.T) {
	var recordsets []huaweiRecordSet
	nextID := 300

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ := io.ReadAll(r.Body)
		sdkDate := r.Header.Get("X-Sdk-Date")
		auth := r.Header.Get("Authorization")
		if sdkDate == "" || !strings.HasPrefix(auth, "SDK-HMAC-SHA256 ") {
			t.Fatalf("missing Huawei headers: %+v", r.Header)
		}
		// Verify SDK-HMAC-SHA256 signature (CanonicalURI must end with /)
		canonURI := r.URL.EscapedPath()
		if !strings.HasSuffix(canonURI, "/") {
			canonURI += "/"
		}
		canonQuery := huaweiCanonicalQuery(r.URL.RawQuery)
		signedMap := map[string]string{"host": r.Host, "x-sdk-date": sdkDate}
		if len(rawBody) > 0 {
			signedMap["content-type"] = r.Header.Get("Content-Type")
		}
		hKeys := make([]string, 0, len(signedMap))
		for k := range signedMap {
			hKeys = append(hKeys, k)
		}
		sort.Strings(hKeys)
		var ch strings.Builder
		for _, k := range hKeys {
			ch.WriteString(k + ":" + signedMap[k] + "\n")
		}
		sh := strings.Join(hKeys, ";")
		cr := r.Method + "\n" + canonURI + "\n" + canonQuery + "\n" + ch.String() + "\n" + sh + "\n" + sha256Hex(rawBody)
		s2s := "SDK-HMAC-SHA256\n" + sdkDate + "\n" + sha256Hex([]byte(cr))
		sig := hex.EncodeToString(hmacSHA256Bytes([]byte("TEST_HW_SK"), s2s))
		want := "SDK-HMAC-SHA256 Access=TEST_HW_AK, SignedHeaders=" + sh + ", Signature=" + sig
		if auth != want {
			t.Fatalf("huawei signature mismatch:\ngot:  %s\nwant: %s", auth, want)
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"zones": []map[string]any{{"id": "hw-zone-1", "name": "example.com."}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v2/zones/hw-zone-1/recordsets":
			name := r.URL.Query().Get("name")
			var matched []huaweiRecordSet
			for _, rs := range recordsets {
				if rs.Name == name {
					matched = append(matched, rs)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"recordsets": matched,
				"metadata":   map[string]any{"total_count": len(matched)},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v2/zones/hw-zone-1/recordsets":
			var body huaweiRecordSet
			_ = json.Unmarshal(rawBody, &body)
			nextID++
			body.ID = "rs-" + strconv.Itoa(nextID)
			recordsets = append(recordsets, body)
			w.WriteHeader(202)
			_ = json.NewEncoder(w).Encode(body)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v2/zones/hw-zone-1/recordsets/"):
			id := strings.TrimPrefix(r.URL.Path, "/v2/zones/hw-zone-1/recordsets/")
			var body huaweiRecordSet
			_ = json.Unmarshal(rawBody, &body)
			for i := range recordsets {
				if recordsets[i].ID == id {
					recordsets[i].Records = body.Records
					recordsets[i].TTL = body.TTL
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v2/zones/hw-zone-1/recordsets/"):
			id := strings.TrimPrefix(r.URL.Path, "/v2/zones/hw-zone-1/recordsets/")
			recordsets = slices.DeleteFunc(recordsets, func(rs huaweiRecordSet) bool { return rs.ID == id })
			w.WriteHeader(202)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	client := NewHuaweiCloudDNS("TEST_HW_AK", "TEST_HW_SK")
	client.BaseURL = srv.URL
	client.Client = srv.Client()

	zoneID, err := client.ZoneID(context.Background(), "example.com")
	if err != nil || zoneID != "hw-zone-1" {
		t.Fatalf("ZoneID failed: %v", err)
	}
	changed, err := client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "nas.example.com", Type: "AAAA"}, netip.MustParseAddr("2001:4860:4860::8888"))
	if err != nil || !changed || len(recordsets) != 1 {
		t.Fatalf("create failed: changed=%v err=%v", changed, err)
	}
	changed, err = client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "nas.example.com", Type: "AAAA"}, netip.MustParseAddr("2001:4860:4860::8888"))
	if err != nil || changed {
		t.Fatalf("unchanged failed: changed=%v err=%v", changed, err)
	}
	changed, err = client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "nas.example.com", Type: "AAAA"}, netip.MustParseAddr("2001:4860:4860::8844"))
	if err != nil || !changed || recordsets[0].Records[0] != "2001:4860:4860::8844" {
		t.Fatalf("update failed: changed=%v err=%v", changed, err)
	}

	// Wildcard + Apex multi-TXT merge & partial cleanup
	adapter := newDNS01ChallengeAdapter(client)
	adapter.findZone = func(string) (string, error) { return "example.com.", nil }
	if err := adapter.Present("*.example.com", "t1", "auth-one"); err != nil {
		t.Fatalf("first Present failed: %v", err)
	}
	if err := adapter.Present("example.com", "t2", "auth-two"); err != nil {
		t.Fatalf("second Present failed: %v", err)
	}
	var txtRS *huaweiRecordSet
	for i := range recordsets {
		if recordsets[i].Type == "TXT" {
			txtRS = &recordsets[i]
		}
	}
	if txtRS == nil || len(txtRS.Records) != 2 {
		t.Fatalf("expected 2 merged TXT values in single Huawei RecordSet, got: %+v", recordsets)
	}
	if err := adapter.CleanUp("*.example.com", "t1", "auth-one"); err != nil {
		t.Fatalf("first CleanUp failed: %v", err)
	}
	// Still 1 record remaining
	txtCount := 0
	for _, rs := range recordsets {
		if rs.Type == "TXT" {
			txtCount = len(rs.Records)
		}
	}
	if txtCount != 1 {
		t.Fatalf("expected 1 TXT record remaining after partial cleanup, got %d", txtCount)
	}
	if err := adapter.CleanUp("example.com", "t2", "auth-two"); err != nil {
		t.Fatalf("second CleanUp failed: %v", err)
	}
	for _, rs := range recordsets {
		if rs.Type == "TXT" {
			t.Fatalf("TXT recordset should be deleted when empty: %+v", rs)
		}
	}
}

func TestPorkbunDNSProviderSyncAndACME(t *testing.T) {
	var records []porkbunRecord
	nextID := 400

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["apikey"] != "TEST_PB_KEY" || body["secretapikey"] != "TEST_PB_SECRET" {
			w.WriteHeader(403)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ERROR"})
			return
		}
		switch {
		case r.URL.Path == "/dns/retrieve/example.com":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "SUCCESS", "records": records})
		case r.URL.Path == "/dns/create/example.com":
			nextID++
			name := "example.com"
			if sub, _ := body["name"].(string); sub != "" {
				name = sub + ".example.com"
			}
			idStr := strconv.Itoa(nextID)
			records = append(records, porkbunRecord{
				ID:      idStr,
				Name:    name,
				Type:    body["type"].(string),
				Content: body["content"].(string),
				TTL:     "600",
			})
			// Return numeric ID for one and string ID if needed; numeric tests flexible decoding.
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "SUCCESS", "id": nextID})
		case strings.HasPrefix(r.URL.Path, "/dns/edit/example.com/"):
			id := strings.TrimPrefix(r.URL.Path, "/dns/edit/example.com/")
			for i := range records {
				if records[i].ID == id {
					records[i].Content = body["content"].(string)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "SUCCESS"})
		case strings.HasPrefix(r.URL.Path, "/dns/delete/example.com/"):
			id := strings.TrimPrefix(r.URL.Path, "/dns/delete/example.com/")
			records = slices.DeleteFunc(records, func(rec porkbunRecord) bool { return rec.ID == id })
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "SUCCESS"})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	client := NewPorkbunDNS("TEST_PB_KEY", "TEST_PB_SECRET")
	client.BaseURL = srv.URL
	client.Client = srv.Client()

	zoneID, err := client.ZoneID(context.Background(), "example.com")
	if err != nil || zoneID != "example.com" {
		t.Fatalf("ZoneID failed: %v", err)
	}
	// Root domain sync
	changed, err := client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "example.com", Type: "A"}, netip.MustParseAddr("1.2.3.4"))
	if err != nil || !changed || len(records) != 1 || records[0].Name != "example.com" {
		t.Fatalf("root create failed: changed=%v err=%v records=%+v", changed, err, records)
	}
	changed, err = client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "example.com", Type: "A"}, netip.MustParseAddr("1.2.3.4"))
	if err != nil || changed {
		t.Fatalf("no-op failed: changed=%v err=%v", changed, err)
	}
	changed, err = client.SyncRecord(context.Background(), zoneID, DNSRecord{Host: "example.com", Type: "A"}, netip.MustParseAddr("5.6.7.8"))
	if err != nil || !changed || records[0].Content != "5.6.7.8" {
		t.Fatalf("edit failed: changed=%v err=%v records=%+v", changed, err, records)
	}

	adapter := newDNS01ChallengeAdapter(client)
	adapter.findZone = func(string) (string, error) { return "example.com.", nil }
	if err := adapter.Present("app.example.com", "t", "pb-auth"); err != nil {
		t.Fatalf("Present failed: %v", err)
	}
	if err := adapter.CleanUp("app.example.com", "t", "pb-auth"); err != nil {
		t.Fatalf("CleanUp failed: %v", err)
	}
	for _, r := range records {
		if r.Type == "TXT" {
			t.Fatalf("TXT record not cleaned up: %+v", records)
		}
	}
}

func TestMultiProviderCredentialsConfigRevealRedactionBackupAndStatus(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	a.logs, _ = NewLogs(t.TempDir(), a.store)

	// 1. /api/status returns all 5 dns_providers
	sw := adminRequest(h, "GET", "/api/status", nil, cookie, "")
	if sw.Code != 200 {
		t.Fatalf("GET /api/status failed: %d", sw.Code)
	}
	var statusResp struct {
		DNSProviders []DNSProviderMeta `json:"dns_providers"`
	}
	if err := json.Unmarshal(sw.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("decode status failed: %v", err)
	}
	if len(statusResp.DNSProviders) != 5 {
		t.Fatalf("expected 5 dns_providers, got %d: %+v", len(statusResp.DNSProviders), statusResp.DNSProviders)
	}

	// 2. Save dual-key DDNS group (alidns) and ACME certificate request (huaweicloud)
	c := DefaultConfig()
	g := testDDNSGroup()
	g.ID = "ali-home"
	g.Provider = "alidns"
	g.Enabled = true
	g2 := testDDNSGroup()
	g2.ID = "tc-home"
	g2.Provider = "tencentcloud"
	g2.Zone = "example.net"
	g2.Hosts = []string{"nas.example.net"}
	g2.Enabled = false
	c.DDNS.Groups = []DDNSGroup{g, g2}
	c.ACME.Enabled, c.ACME.AcceptTerms = true, true
	c.ACME.Email = "admin@example.com"
	c.ACME.Requests = []CertificateRequest{
		{ID: "hw-cert", Provider: "huaweicloud", Domains: []string{"*.example.com"}, Enabled: true},
	}

	aliKey, aliSecret := "TEST_ONLY_ALI_KEY_ID", "TEST_ONLY_ALI_SECRET"
	hwKey, hwSecret := "TEST_ONLY_HW_AK", "TEST_ONLY_HW_SK"

	// Missing key_id on enabled dual-key DDNS must be rejected
	badW := adminRequest(h, "PUT", "/api/config", map[string]any{
		"config":   c,
		"revision": 1,
		"dns_credentials": map[string]any{
			g.ID: map[string]string{"token": aliSecret},
		},
		"certificate_credentials": map[string]any{
			"hw-cert": map[string]string{"key_id": hwKey, "token": hwSecret},
		},
	}, cookie, "")
	if badW.Code != 400 {
		t.Fatalf("expected 400 for missing dual key_id, got %d", badW.Code)
	}

	// Complete dual-key save succeeds and never discloses secrets
	w := adminRequest(h, "PUT", "/api/config", map[string]any{
		"config":   c,
		"revision": 1,
		"dns_credentials": map[string]any{
			g.ID: map[string]string{"key_id": aliKey, "token": aliSecret},
		},
		"certificate_credentials": map[string]any{
			"hw-cert": map[string]string{"key_id": hwKey, "token": hwSecret},
		},
	}, cookie, "")
	if w.Code != 200 {
		t.Fatalf("dual-key save failed: %d %s", w.Code, w.Body.String())
	}
	for _, secret := range []string{aliKey, aliSecret, hwKey, hwSecret} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("PUT /api/config leaked secret %q", secret)
		}
	}
	for _, flag := range []string{
		`"dns_credentials_configured":{"ali-home":true,"tc-home":false}`,
		`"dns_key_configured":{"ali-home":true,"tc-home":false}`,
		`"dns_token_configured":{"ali-home":true,"tc-home":false}`,
		`"certificate_credentials_configured":{"hw-cert":true}`,
		`"certificate_key_configured":{"hw-cert":true}`,
		`"certificate_token_configured":{"hw-cert":true}`,
	} {
		if !strings.Contains(w.Body.String(), flag) {
			t.Fatalf("missing expected configured flag %s in %s", flag, w.Body.String())
		}
	}

	// 3. Partial patch: update only token preserves existing key_id
	aliSecret2 := "TEST_ONLY_ALI_SECRET_V2"
	w = adminRequest(h, "PUT", "/api/config", map[string]any{
		"config":   c,
		"revision": 2,
		"dns_credentials": map[string]any{
			g.ID: map[string]string{"token": aliSecret2},
		},
	}, cookie, "")
	if w.Code != 200 {
		t.Fatalf("partial patch failed: %d %s", w.Code, w.Body.String())
	}
	snap := a.store.Snapshot()
	if snap.DNSCredentials[g.ID].KeyID != aliKey || snap.DNSCredentials[g.ID].Token != aliSecret2 {
		t.Fatalf("partial patch clobbered key_id or missed token: %+v", snap.DNSCredentials[g.ID])
	}

	// 4. Reveal endpoints return exact key_id and token
	for _, req := range []struct{ kind, id, want string }{
		{"ddns-key", g.ID, aliKey},
		{"ddns", g.ID, aliSecret2},
		{"certificate-key", "hw-cert", hwKey},
		{"certificate", "hw-cert", hwSecret},
	} {
		rw := adminRequest(h, "POST", "/api/credentials/reveal", map[string]string{"kind": req.kind, "id": req.id}, cookie, "")
		var res map[string]string
		if rw.Code != 200 || json.Unmarshal(rw.Body.Bytes(), &res) != nil || res["value"] != req.want {
			t.Fatalf("reveal %s/%s failed: code=%d body=%s want=%q", req.kind, req.id, rw.Code, rw.Body.String(), req.want)
		}
	}

	// 5. Log redaction scrubs both KeyID and Token
	a.logs.Add(LogEntry{Category: "ddns", Message: "leaked " + aliKey + " and " + aliSecret2 + " and " + hwKey})
	rows, _, _ := a.logs.List("ddns", "", 0, 10)
	if len(rows) != 1 || strings.Contains(rows[0].Message, aliKey) || strings.Contains(rows[0].Message, aliSecret2) || strings.Contains(rows[0].Message, hwKey) {
		t.Fatalf("logs did not redact dual-key secrets: %+v", rows)
	}

	// 6. Encrypted backup round-trip retains KeyID and Token
	data, err := encodeBackup(backupPayload{State: snap, Certificates: map[string][]byte{}}, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || bytes.Contains(data, []byte(aliKey)) || bytes.Contains(data, []byte(aliSecret2)) {
		t.Fatal("backup leaked secrets")
	}
	restored, _, err := decodeBackup(data, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || restored.State.DNSCredentials[g.ID].KeyID != aliKey || restored.State.CertificateCredentials["hw-cert"].KeyID != hwKey {
		t.Fatalf("backup round-trip lost dual-key credentials: %+v", restored.State)
	}

	// 7. Switching provider clears previous provider's credentials
	c.DDNS.Groups[0].Provider = "porkbun"
	c.DDNS.Groups[0].Enabled = false
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 3}, cookie, "")
	if w.Code != 200 {
		t.Fatalf("provider switch failed: %d", w.Code)
	}
	if cred := a.store.Snapshot().DNSCredentials[g.ID]; cred.HasAny() {
		t.Fatalf("expected credentials cleared on provider switch, got %+v", cred)
	}

	// 8. newDNSProvider constructs all 4 new providers cleanly
	for _, provider := range []string{"alidns", "tencentcloud", "huaweicloud", "porkbun"} {
		st := State{
			Config: DefaultConfig(),
			DNSCredentials: map[string]DNSCredential{
				"g1": {KeyID: "k1", Token: "t1"},
			},
		}
		p, err := newDNSProvider(st, DDNSGroup{ID: "g1", Provider: provider}, nil)
		if err != nil {
			t.Fatalf("newDNSProvider(%q) failed: %v", provider, err)
		}
		p.Close()
	}
}
