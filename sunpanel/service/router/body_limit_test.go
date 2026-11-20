package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/ini.v1"
	"sun-panel/global"
	"sun-panel/lib/iniConfig"
	"sun-panel/lib/language"
)

func TestNoticeBodyLimit(t *testing.T) {
	oldLang := global.Lang
	global.Lang = &language.LangStructObj{LangContet: &iniConfig.IniConfig{Config: ini.Empty()}}
	t.Cleanup(func() { global.Lang = oldLang })
	handler := NewRouter(http.Dir(t.TempDir()))
	for _, chunked := range []bool{false, true} {
		for _, size := range []int{64 << 10, (64 << 10) + 1} {
			// Invalid JSON exercises the normal validation response without needing a database.
			r := httptest.NewRequest("POST", "/sunpanel/api/notice/getListByDisplayType", strings.NewReader(strings.Repeat(" ", size)))
			if chunked {
				r.ContentLength = -1
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if (w.Code == 413) != (size > 64<<10) || w.Code >= 500 {
				t.Fatalf("size %d, chunked %v: HTTP %d", size, chunked, w.Code)
			}
		}
	}
}
