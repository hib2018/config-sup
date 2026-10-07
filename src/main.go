package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func handler(port string) http.Handler {
	assets := map[string]string{"/": "index.html", "/style.css": "style.css", "/app.js": "app.js"}
	plans := map[string]*plan{}
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "127.0.0.1:"+port {
			http.NotFound(w, r)
			return
		}
		file, ok := assets[r.URL.Path]
		if !ok || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if file == "app.js" {
			http.ServeFile(w, r, filepath.Join("public", file))
		} else {
			http.ServeFile(w, r, filepath.Join("src", "web", file))
		}
	})
	post := func(w http.ResponseWriter, r *http.Request, input any) bool {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Host != "127.0.0.1:"+port || r.Header.Get("Origin") != "http://127.0.0.1:"+port || strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) != "application/json" {
			respondError(w, fmt.Errorf("許可されていないリクエストです"))
			return false
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 32_001))
		if err != nil || len(data) > 32_000 || json.Unmarshal(data, input) != nil {
			respondError(w, fmt.Errorf("リクエストが不正です"))
			return false
		}
		return true
	}
	lookup := func(token string) *plan {
		mu.Lock()
		defer mu.Unlock()
		p := plans[token]
		if p != nil && time.Since(p.Created) > 20*time.Minute {
			delete(plans, token)
			return nil
		}
		return p
	}
	mux.HandleFunc("POST /analyze", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			URL      string `json:"url"`
			UseAgent bool   `json:"useAgent"`
			UseLocal bool   `json:"useLocal"`
		}
		if !post(w, r, &input) {
			return
		}
		if !input.UseLocal {
			respondError(w, fmt.Errorf("ローカル設定の検索とキー情報のモデルへの送信に同意してください"))
			return
		}
		result, p, err := analyze(input.URL, input.UseAgent)
		if err != nil {
			respondError(w, err)
			return
		}
		mu.Lock()
		for token, old := range plans {
			if time.Since(old.Created) > 20*time.Minute {
				delete(plans, token)
			}
		}
		if len(plans) >= 20 {
			mu.Unlock()
			respondError(w, fmt.Errorf("解析セッションが多すぎます。再起動してください"))
			return
		}
		plans[p.Token] = p
		mu.Unlock()
		json.NewEncoder(w).Encode(result)
	})
	mux.HandleFunc("POST /preview", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token   string          `json:"token"`
			Changes []pendingChange `json:"changes"`
		}
		if !post(w, r, &input) {
			return
		}
		p := lookup(input.Token)
		if p == nil {
			respondError(w, fmt.Errorf("解析結果が失効しました。再解析してください"))
			return
		}
		id, diff, err := p.preview(input.Changes)
		if err != nil {
			respondError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"previewId": id, "diff": diff})
	})
	mux.HandleFunc("POST /apply", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token     string `json:"token"`
			PreviewID string `json:"previewId"`
		}
		if !post(w, r, &input) {
			return
		}
		p := lookup(input.Token)
		if p == nil {
			respondError(w, fmt.Errorf("解析結果が失効しました。再解析してください"))
			return
		}
		if err := p.apply(input.PreviewID); err != nil {
			respondError(w, err)
			return
		}
		mu.Lock()
		delete(plans, input.Token)
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]bool{"applied": true})
	})
	return mux
}

func respondError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4173"
	}
	address := "127.0.0.1:" + port
	fmt.Println("http://" + address)
	if err := http.ListenAndServe(address, handler(port)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
