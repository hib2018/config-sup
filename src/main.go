package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func handler(port string) http.Handler {
	assets := map[string]string{"/": "index.html", "/style.css": "style.css", "/app.js": "app.js"}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("POST /analyze", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != "http://127.0.0.1:"+port || strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) != "application/json" {
			respondError(w, fmt.Errorf("許可されていないリクエストです"))
			return
		}
		var input struct {
			URL      string `json:"url"`
			UseAgent bool   `json:"useAgent"`
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 3001))
		if err != nil {
			respondError(w, err)
			return
		}
		if len(data) > 3000 {
			respondError(w, fmt.Errorf("リクエストが大きすぎます"))
			return
		}
		if err = json.Unmarshal(data, &input); err != nil {
			respondError(w, err)
			return
		}
		result, err := analyze(input.URL, input.UseAgent)
		if err != nil {
			respondError(w, err)
			return
		}
		json.NewEncoder(w).Encode(result)
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
