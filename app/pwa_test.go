package main

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func webAssetsTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	router := gin.New()
	registerWebAssets(router)
	return router
}

func TestPWAInstallationAssets(t *testing.T) {
	router := webAssetsTestRouter(t)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/manifest+json" || response.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("manifest must be publicly served with its MIME type and revalidation: status %d", response.Code)
	}
	var manifest struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		ShortName       string `json:"short_name"`
		StartURL        string `json:"start_url"`
		Scope           string `json:"scope"`
		Display         string `json:"display"`
		ThemeColor      string `json:"theme_color"`
		BackgroundColor string `json:"background_color"`
		ShareTarget     struct {
			Action  string `json:"action"`
			Method  string `json:"method"`
			Enctype string `json:"enctype"`
			Params  struct {
				Title string `json:"title"`
				Text  string `json:"text"`
				URL   string `json:"url"`
				Files []struct {
					Name   string   `json:"name"`
					Accept []string `json:"accept"`
				} `json:"files"`
			} `json:"params"`
		} `json:"share_target"`
		Icons           []struct {
			Src     string `json:"src"`
			Sizes   string `json:"sizes"`
			Type    string `json:"type"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "/tray/" || manifest.StartURL != "/tray/" || manifest.Scope != "/" || manifest.Name != "DocTray" || manifest.ShortName != "DocTray" || manifest.Display != "standalone" || manifest.ThemeColor != "#335599" || manifest.BackgroundColor != "#ffffff" {
		t.Error("installation must retain stable identity, launch at the tray and cover the local login/navigation routes")
	}
	share := manifest.ShareTarget
	if share.Action != "/tray/share" || share.Method != "POST" || share.Enctype != "multipart/form-data" || share.Params.Title != "title" || share.Params.Text != "text" || share.Params.URL != "url" {
		t.Error("Android must send text/link shares to the implemented multipart receiver")
	}
	if len(share.Params.Files) != 1 || share.Params.Files[0].Name != "files" || len(share.Params.Files[0].Accept) != 1 || share.Params.Files[0].Accept[0] != "*/*" {
		t.Error("Android file shares must use the files field and accept images and other file types")
	}
	if len(manifest.Icons) != 2 {
		t.Fatal("installation needs both 192px and 512px PNG icons")
	}
	wantSizes := map[string]int{"192x192": 192, "512x512": 512}
	for _, icon := range manifest.Icons {
		size, ok := wantSizes[icon.Sizes]
		if !ok || icon.Type != "image/png" || icon.Purpose != "any" {
			t.Fatalf("unexpected icon configuration: %+v", icon)
		}
		delete(wantSizes, icon.Sizes)
		image := httptest.NewRecorder()
		router.ServeHTTP(image, httptest.NewRequest(http.MethodGet, icon.Src, nil))
		if image.Code != http.StatusOK || image.Header().Get("Cache-Control") != "no-cache" || image.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("icon %s must be publicly available with revalidation", icon.Src)
		}
		config, err := png.DecodeConfig(bytes.NewReader(image.Body.Bytes()))
		if err != nil || config.Width != size || config.Height != size {
			t.Errorf("icon %s must actually be %dx%d: dimensions %+v, error %v", icon.Src, size, size, config, err)
		}
	}
	// Installation fetches should not create a login session or require a GET body.
	head := httptest.NewRecorder()
	router.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/manifest.webmanifest", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != "application/manifest+json" || head.Header().Get("Set-Cookie") != "" {
		t.Error("manifest HEAD must remain public and bodyless")
	}
}

func TestWebCachePolicy(t *testing.T) {
	router := webAssetsTestRouter(t)
	for _, route := range []string{"/", "/tray/", "/media/attachment", "/login", "/callback", "/logout"} {
		router.GET(route, func(c *gin.Context) { c.String(http.StatusOK, "private response") })
	}
	router.POST("/tray/doc-create", func(c *gin.Context) { c.String(http.StatusOK, "private fragment") })
	for _, test := range []struct {
		method string
		path   string
		cache  string
	}{
		{http.MethodGet, "/", "no-store"},
		{http.MethodGet, "/tray/", "no-store"},
		{http.MethodGet, "/media/attachment", "no-store"},
		{http.MethodGet, "/login", "no-store"},
		{http.MethodGet, "/callback", "no-store"},
		{http.MethodGet, "/logout", "no-store"},
		{http.MethodPost, "/tray/doc-create", "no-store"},
		{http.MethodGet, "/resources/css/main.css", "no-cache"},
		{http.MethodGet, "/resources/scripts/dragondrop.js", "no-cache"},
		{http.MethodGet, "/favicon.ico", "no-cache"},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != test.cache {
				t.Errorf("status/cache = %d/%q, want 200/%q", response.Code, response.Header().Get("Cache-Control"), test.cache)
			}
		})
	}
}
