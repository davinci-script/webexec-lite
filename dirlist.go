package main

import (
	"html/template"
	"net/http"
	"os"
	"sort"
)

type fileInfo struct {
	Name   string
	IsDir  bool
	Size   int64
	ModTime string
}

func RenderDirList(w http.ResponseWriter, r *http.Request, dirPath, urlPath string) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("Failed to read directory."))
		return
	}
	var infos []fileInfo
	for _, f := range files {
		info, _ := f.Info()
		infos = append(infos, fileInfo{
			Name:   f.Name(),
			IsDir:  f.IsDir(),
			Size:   info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	tmplPath := "html/dirlist.html"
	tmplContent, err := os.ReadFile(tmplPath)
	var t *template.Template
	if err == nil {
		t, err = template.New("dir").Parse(string(tmplContent))
	}
	if err != nil || t == nil {
		// fallback to built-in minimal template
		t, _ = template.New("dir").Parse(`<html><head><title>Index of {{.Path}}</title></head><body><h1>Index of {{.Path}}</h1><ul>{{range .Files}}<li><a href="{{$.Prefix}}{{.Name}}{{if .IsDir}}/{{end}}">{{.Name}}{{if .IsDir}}/{{end}}</a></li>{{end}}</ul></body></html>`)
	}
	_ = t.Execute(w, map[string]any{"Path": urlPath, "Files": infos, "Prefix": template.URLQueryEscaper(urlPath)})
} 