package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type StatusWriter struct {
	http.ResponseWriter
	Status int
	Bytes  int
}

func (w *StatusWriter) WriteHeader(code int) {
	w.Status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *StatusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.Bytes += n
	return n, err
}

func OpenLogFile(path string) *os.File {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to open log file %s: %v", path, err)
		return nil
	}
	return f
}

func LogAccess(r *http.Request, ww *StatusWriter, accessLogger *log.Logger) {
	remoteHost := r.RemoteAddr
	if idx := strings.LastIndex(remoteHost, ":"); idx != -1 {
		remoteHost = remoteHost[:idx]
	}
	user := "-"
	identd := "-"
	timeStr := time.Now().Format("02/Jan/2006:15:04:05 -0700")
	requestLine := r.Method + " " + r.URL.RequestURI() + " " + r.Proto
	status := ww.Status
	bytes := ww.Bytes
	referer := r.Referer()
	if referer == "" {
		referer = "-"
	}
	userAgent := r.UserAgent()
	if userAgent == "" {
		userAgent = "-"
	}
	logMsg :=
		remoteHost + " " + identd + " " + user + " [" + timeStr + "] \"" + requestLine + "\" " +
			itoa(status) + " " + itoa(bytes) + " \"" + referer + "\" \"" + userAgent + "\""
	if accessLogger != nil {
		accessLogger.Println(logMsg)
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
} 