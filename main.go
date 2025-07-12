package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type ErrorPages struct {
	NotFound string `json:"404"`
	Internal string `json:"500"`
}

type HandlerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type Config struct {
	HomeDir        string                  `json:"homedir"`
	Port           string                  `json:"port"`
	ErrorPages     ErrorPages              `json:"error_pages"`
	DefaultIndexes []string                `json:"default_indexes"`
	Handlers       map[string]HandlerConfig `json:"handlers"`
}

func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var cfg Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func serveErrorPage(w http.ResponseWriter, code int, pagePath string, defaultMsg string) {
	w.WriteHeader(code)
	if pagePath != "" {
		if data, err := ioutil.ReadFile(pagePath); err == nil {
			w.Write(data)
			return
		}
	}
	w.Write([]byte(defaultMsg))
}

func tryServeIndex(w http.ResponseWriter, r *http.Request, dirPath string, indexes []string) bool {
	for _, idx := range indexes {
		indexPath := filepath.Join(dirPath, idx)
		if stat, err := os.Stat(indexPath); err == nil && !stat.IsDir() {
			http.ServeFile(w, r, indexPath)
			return true
		}
	}
	return false
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	mode := info.Mode()
	return mode&0111 != 0 // any execute bit set
}

func resolveHandlerCommand(cmdPath string) string {
	if filepath.IsAbs(cmdPath) {
		return cmdPath
	}
	abs, err := filepath.Abs(cmdPath)
	if err != nil {
		return cmdPath // fallback to original
	}
	return abs
}

func handleWithExternal(w http.ResponseWriter, r *http.Request, handler HandlerConfig, filePath string) {
	cmdPath := resolveHandlerCommand(handler.Command)
	if !isExecutable(cmdPath) {
		w.WriteHeader(500)
		w.Write([]byte("Handler executable not found or not executable: " + cmdPath))
		return
	}
	args := make([]string, len(handler.Args))
	for i, arg := range handler.Args {
		args[i] = strings.ReplaceAll(arg, "{filepath}", filePath)
	}
	cmd := exec.Command(cmdPath, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = r.Body
	output, err := cmd.Output()
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("Handler error: " + err.Error()))
		return
	}
	w.Write(output)
}

func tryServeIndexWithHandler(w http.ResponseWriter, r *http.Request, dirPath string, indexes []string, handlers map[string]HandlerConfig) bool {
	for _, idx := range indexes {
		indexPath := filepath.Join(dirPath, idx)
		if stat, err := os.Stat(indexPath); err == nil && !stat.IsDir() {
			ext := strings.ToLower(filepath.Ext(indexPath))
			if handler, ok := handlers[ext]; ok {
				handleWithExternal(w, r, handler, indexPath)
				return true
			}
			http.ServeFile(w, r, indexPath)
			return true
		}
	}
	return false
}

func main() {
	configPath := flag.String("config", "config.json", "Path to config file")
	homeDirFlag := flag.String("homedir", "", "Directory to serve static files from")
	portFlag := flag.String("port", "", "Port to serve HTTP on")
	flag.Parse()

	cfg := &Config{
		HomeDir: "./public",
		Port:    "80",
		ErrorPages: ErrorPages{
			NotFound: "./public/404.html",
			Internal: "./public/500.html",
		},
		DefaultIndexes: []string{"index.html", "index.htm"},
		Handlers:       make(map[string]HandlerConfig),
	}

	if _, err := os.Stat(*configPath); err == nil {
		if fileCfg, err := loadConfig(*configPath); err == nil {
			if fileCfg.HomeDir != "" {
				cfg.HomeDir = fileCfg.HomeDir
			}
			if fileCfg.Port != "" {
				cfg.Port = fileCfg.Port
			}
			if fileCfg.ErrorPages.NotFound != "" {
				cfg.ErrorPages.NotFound = fileCfg.ErrorPages.NotFound
			}
			if fileCfg.ErrorPages.Internal != "" {
				cfg.ErrorPages.Internal = fileCfg.ErrorPages.Internal
			}
			if len(fileCfg.DefaultIndexes) > 0 {
				cfg.DefaultIndexes = fileCfg.DefaultIndexes
			}
			if len(fileCfg.Handlers) > 0 {
				cfg.Handlers = fileCfg.Handlers
			}
		}
	}

	if *homeDirFlag != "" {
		cfg.HomeDir = *homeDirFlag
	}
	if *portFlag != "" {
		cfg.Port = *portFlag
	}

	addr := ":" + cfg.Port
	server := &http.Server{Addr: addr}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		filePath := cfg.HomeDir + r.URL.Path
		if stat, err := os.Stat(filePath); err == nil {
			if stat.IsDir() {
				if tryServeIndexWithHandler(w, r, filePath, cfg.DefaultIndexes, cfg.Handlers) {
					return
				}
				serveErrorPage(w, 404, cfg.ErrorPages.NotFound, "404 page not found")
				return
			}
			ext := strings.ToLower(filepath.Ext(filePath))
			if handler, ok := cfg.Handlers[ext]; ok {
				handleWithExternal(w, r, handler, filePath)
				return
			}
			http.ServeFile(w, r, filePath)
			return
		}
		serveErrorPage(w, 404, cfg.ErrorPages.NotFound, "404 page not found")
	})

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Printf("Serving %s on HTTP port: %s\n", cfg.HomeDir, cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Println("Server failed:", err)
		}
	}()

	<-quit
	fmt.Println("\nShutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		fmt.Println("Server forced to shutdown:", err)
	} else {
		fmt.Println("Server stopped gracefully.")
	}
}

