package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Authenticate with the worker before fetching anything, then use its normal
// upload API so ownership, storage, file management and backups stay identical.
func (a *Admin) sunPanelImageImport(w http.ResponseWriter, r *http.Request) {
	reply := func(code int, message string) {
		jsonResponse(w, 200, map[string]any{"code": code, "msg": message})
	}
	if r.Header.Get("token") == "" {
		reply(1000, "请先登录 Sun-Panel")
		return
	}
	a.sunPanel.mu.Lock()
	address := a.sunPanel.bridgeURL
	a.sunPanel.mu.Unlock()
	if address == "" {
		reply(-1, "Sun-Panel 未运行，请稍后重试")
		return
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	auth, _ := http.NewRequestWithContext(r.Context(), "POST", address+"/sunpanel/api/user/getInfo", nil)
	auth.Header.Set("token", r.Header.Get("token"))
	response, err := client.Do(auth)
	if err != nil {
		reply(-1, "Sun-Panel 登录状态检查失败，请重试")
		return
	}
	var result struct {
		Code *int `json:"code"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 32<<10)).Decode(&result)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || result.Code == nil || *result.Code != 0 {
		reply(1001, "Sun-Panel 登录已过期，请重新登录")
		return
	}
	var input struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		reply(-1, "图片 URL 请求无效")
		return
	}
	data, err := downloadRouteImageWithProxy(r.Context(), input.URL, a.store.Snapshot())
	if err == nil {
		data, err = normalizeRouteImage(data)
	}
	if err != nil {
		reply(-1, err.Error())
		return
	}
	name := "remote-icon.png"
	if http.DetectContentType(data) == "image/x-icon" || http.DetectContentType(data) == "image/vnd.microsoft.icon" {
		name = "remote-icon.ico"
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("imgfile", name)
	part.Write(data)
	writer.Close()
	upload, _ := http.NewRequestWithContext(r.Context(), "POST", address+"/sunpanel/api/file/uploadImg", &body)
	upload.Header.Set("Content-Type", writer.FormDataContentType())
	upload.Header.Set("token", r.Header.Get("token"))
	response, err = client.Do(upload)
	if err != nil {
		reply(-1, "图片上传失败，请重试")
		return
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	io.Copy(w, io.LimitReader(response.Body, 32<<10))
}
