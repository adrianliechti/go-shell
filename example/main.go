// Command example showcases the go-shell window: JavaScript dialogs, file
// pickers, uploads, downloads, external links, zoom and the authenticated
// loopback server.
//
//	go run ./example
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	shell "github.com/adrianliechti/go-shell"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, page)
	})

	mux.HandleFunc("GET /api/time", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"time": time.Now().Format(time.RFC3339)})
	})

	// Served with Content-Disposition — the shell turns this navigation into
	// a download to ~/Downloads.
	mux.HandleFunc("GET /download.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="example.csv"`)
		fmt.Fprintf(w, "id,name\n1,go-shell\n2,example\n")
	})

	mux.HandleFunc("POST /api/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var files []map[string]any

		for _, headers := range r.MultipartForm.File {
			for _, header := range headers {
				files = append(files, map[string]any{"name": header.Filename, "size": header.Size})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(files)
	})

	err := shell.Run(shell.Options{
		Title:   "Shell Example",
		Handler: mux,

		Width:  860,
		Height: 680,

		MinWidth:  480,
		MinHeight: 360,

		Debug: os.Getenv("SHELL_DEBUG") != "",
	})

	if err != nil {
		log.Fatal(err)
	}

	// Run returns when the window closes — cleanup (closing connections,
	// flushing state, ...) belongs here.
	log.Println("window closed, shutting down")
}

const page = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Shell Example</title>
<style>
  :root { color-scheme: light dark; }
  body {
    margin: 0; padding: 2rem; font: 15px/1.5 -apple-system, "Segoe UI", sans-serif;
    background: light-dark(#f5f5f7, #1c1c1e); color: light-dark(#1c1c1e, #f5f5f7);
  }
  h1 { font-size: 1.4rem; }
  p.hint { opacity: .65; margin-top: -.5rem; }
  section {
    background: light-dark(#fff, #2c2c2e); border-radius: 12px;
    padding: 1rem 1.25rem; margin: 1rem 0; box-shadow: 0 1px 3px rgba(0,0,0,.12);
  }
  section h2 { font-size: 1rem; margin: 0 0 .5rem; }
  button, input[type=file]::file-selector-button {
    font: inherit; padding: .35rem .9rem; margin: 0 .5rem .25rem 0;
    border: none; border-radius: 8px; background: #6366f1; color: #fff; cursor: pointer;
  }
  button:hover, input[type=file]::file-selector-button:hover { background: #4f46e5; }
  output { display: block; margin-top: .5rem; opacity: .8; font-size: .9rem; white-space: pre-wrap; }
  a { color: #6366f1; }
</style>
</head>
<body>
<h1>go-shell example</h1>
<p class="hint">A plain web app in a native window — served on an authenticated loopback port. Try Cmd/Ctrl+R to reload and Cmd +/− to zoom.</p>

<section>
  <h2>JavaScript dialogs</h2>
  <button onclick="alert('Hello from a native alert.')">alert()</button>
  <button onclick="dialogOut.value = 'confirm: ' + confirm('Proceed with the example?')">confirm()</button>
  <button onclick="dialogOut.value = 'prompt: ' + prompt('Name this window', 'Shell Example')">prompt()</button>
  <output id="dialogOut"></output>
</section>

<section>
  <h2>File picker &amp; upload</h2>
  <input type="file" id="files" multiple>
  <output id="uploadOut"></output>
</section>

<section>
  <h2>Downloads</h2>
  <button onclick="location.href='/download.csv'">Server download (Content-Disposition)</button>
  <button onclick="blobDownload()">Client download (blob + download attribute)</button>
  <output>Files land in your Downloads folder.</output>
</section>

<section>
  <h2>Server &amp; links</h2>
  <button onclick="fetchTime()">Fetch /api/time</button>
  <a href="https://github.com/adrianliechti/go-shell" target="_blank">Open GitHub in the default browser</a>
  <output id="timeOut"></output>
</section>

<script>
  files.addEventListener('change', async () => {
    const body = new FormData();
    for (const file of files.files) body.append('files', file);
    const response = await fetch('/api/upload', { method: 'POST', body });
    uploadOut.value = 'server received: ' + JSON.stringify(await response.json());
  });

  function blobDownload() {
    const blob = new Blob(['generated on ' + new Date().toISOString() + '\n'], { type: 'text/plain' });
    const anchor = document.createElement('a');
    anchor.href = URL.createObjectURL(blob);
    anchor.download = 'example.txt';
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
  }

  async function fetchTime() {
    const response = await fetch('/api/time');
    timeOut.value = JSON.stringify(await response.json());
  }
</script>
</body>
</html>
`
