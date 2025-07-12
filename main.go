package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
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
	AccessLog      string                  `json:"access_log"`
	ErrorLog       string                  `json:"error_log"`
	HandlerLog     string                  `json:"handler_log"`
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

func openLogFile(path string) *os.File {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to open log file %s: %v", path, err)
		return nil
	}
	return f
}

func handleWithExternal(w http.ResponseWriter, r *http.Request, handler HandlerConfig, filePath string, handlerLogger *log.Logger) {
	cmdPath := resolveHandlerCommand(handler.Command)
	if !isExecutable(cmdPath) {
		w.WriteHeader(500)
		w.Write([]byte("Handler executable not found or not executable: " + cmdPath))
		if handlerLogger != nil {
			handlerLogger.Printf("%s | %s | %v | %s | %s %s | %s | status=%d", time.Now().Format(time.RFC3339), cmdPath, handler.Args, filePath, r.Method, r.URL.RequestURI(), r.RemoteAddr, 500)
		}
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
	status := 200
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("Handler error: " + err.Error()))
		status = 500
	}
	w.Write(output)
	if handlerLogger != nil {
		handlerLogger.Printf("%s | %s | %v | %s | %s %s | %s | status=%d", time.Now().Format(time.RFC3339), cmdPath, args, filePath, r.Method, r.URL.RequestURI(), r.RemoteAddr, status)
	}
}

func tryServeIndexWithHandler(w http.ResponseWriter, r *http.Request, dirPath string, indexes []string, handlers map[string]HandlerConfig) bool {
	for _, idx := range indexes {
		indexPath := filepath.Join(dirPath, idx)
		if stat, err := os.Stat(indexPath); err == nil && !stat.IsDir() {
			ext := strings.ToLower(filepath.Ext(indexPath))
			if handler, ok := handlers[ext]; ok {
				handleWithExternal(w, r, handler, indexPath, nil) // Pass nil for handlerLogger as it's not used here
				return true
			}
			http.ServeFile(w, r, indexPath)
			return true
		}
	}
	return false
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
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
			if fileCfg.AccessLog != "" {
				cfg.AccessLog = fileCfg.AccessLog
			}
			if fileCfg.ErrorLog != "" {
				cfg.ErrorLog = fileCfg.ErrorLog
			}
			if fileCfg.HandlerLog != "" {
				cfg.HandlerLog = fileCfg.HandlerLog
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

	accessLogPath := "access.log"
	errorLogPath := "error.log"
	if cfg.AccessLog != "" {
		accessLogPath = cfg.AccessLog
	}
	if cfg.ErrorLog != "" {
		errorLogPath = cfg.ErrorLog
	}
	accessLog := openLogFile(accessLogPath)
	errorLog := openLogFile(errorLogPath)
	defer func() {
		if accessLog != nil {
			accessLog.Close()
		}
		if errorLog != nil {
			errorLog.Close()
		}
	}()
	accessLogger := log.New(accessLog, "", log.LstdFlags)
	errorLogger := log.New(errorLog, "", log.LstdFlags)

	handlerLogPath := "handler.log"
	if cfg.HandlerLog != "" {
		handlerLogPath = cfg.HandlerLog
	}
	handlerLog := openLogFile(handlerLogPath)
	defer func() {
		if handlerLog != nil {
			handlerLog.Close()
		}
	}()
	handlerLogger := log.New(handlerLog, "", log.LstdFlags)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		filePath := cfg.HomeDir + r.URL.Path
		logAccess := func(ww *statusWriter) {
			remoteHost := r.RemoteAddr
			if idx := strings.LastIndex(remoteHost, ":"); idx != -1 {
				remoteHost = remoteHost[:idx]
			}
			user := "-"
			identd := "-"
			timeStr := time.Now().Format("02/Jan/2006:15:04:05 -0700")
			requestLine := fmt.Sprintf("%s %s %s", r.Method, r.URL.RequestURI(), r.Proto)
			status := ww.status
			bytes := ww.bytes
			referer := r.Referer()
			if referer == "" {
				referer = "-"
			}
			userAgent := r.UserAgent()
			if userAgent == "" {
				userAgent = "-"
			}
			logMsg := fmt.Sprintf("%s %s %s [%s] \"%s\" %d %d \"%s\" \"%s\"", remoteHost, identd, user, timeStr, requestLine, status, bytes, referer, userAgent)
			if accessLogger != nil {
				accessLogger.Println(logMsg)
			}
		}
		if stat, err := os.Stat(filePath); err == nil {
			if stat.IsDir() {
				ww := &statusWriter{ResponseWriter: w, status: 200}
				if tryServeIndexWithHandler(ww, r, filePath, cfg.DefaultIndexes, cfg.Handlers) {
					logAccess(ww)
					return
				}
				ww.status = 404
				serveErrorPage(ww, 404, cfg.ErrorPages.NotFound, "404 page not found")
				if errorLogger != nil {
					errorLogger.Printf("%s %s %d %s", r.Method, r.URL.Path, ww.status, r.RemoteAddr)
				}
				logAccess(ww)
				return
			}
			ext := strings.ToLower(filepath.Ext(filePath))
			if handler, ok := cfg.Handlers[ext]; ok {
				ww := &statusWriter{ResponseWriter: w, status: 200}
				handleWithExternal(ww, r, handler, filePath, handlerLogger)
				if ww.status >= 400 && errorLogger != nil {
					errorLogger.Printf("%s %s %d %s", r.Method, r.URL.Path, ww.status, r.RemoteAddr)
				}
				logAccess(ww)
				return
			}
			ww := &statusWriter{ResponseWriter: w, status: 200}
			http.ServeFile(ww, r, filePath)
			logAccess(ww)
			return
		}
		ww := &statusWriter{ResponseWriter: w, status: 404}
		serveErrorPage(ww, 404, cfg.ErrorPages.NotFound, "404 page not found")
		if errorLogger != nil {
			errorLogger.Printf("%s %s %d %s", r.Method, r.URL.Path, ww.status, r.RemoteAddr)
		}
		logAccess(ww)
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

