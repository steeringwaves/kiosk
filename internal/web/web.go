package web

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"kiosk/internal/config"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

//go:embed static
var _staticFS embed.FS
var staticFS, _ = fs.Sub(_staticFS, "static")

//go:embed templates
var templateFS embed.FS

type IKiosk interface {
	AddDisplay(display config.DisplayConfig) error
	RemoveDisplay(name string) error
	AddTab(displayName string, tab config.TabConfig) error
	RemoveTab(displayName, tabURL string) error
	EditDisplay(display config.DisplayConfig) error
	EditTab(displayName string, tab config.TabConfig) error
	ReloadDisplays() error
}

var (
	mu        sync.Mutex
	templates *template.Template
)

// Utility
func parseFormInt(r *http.Request, key string) int {
	val, _ := strconv.Atoi(r.FormValue(key))
	return val
}

// Routes
func (kiosk *KioskWeb) index(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, staticFS, "index.html")
}

func (kiosk *KioskWeb) getDisplayList(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	var list []config.DisplayConfig
	for _, d := range kiosk.cfg.Displays {
		list = append(list, d)
	}
	err := templates.ExecuteTemplate(w, "display_list.html", struct {
		Displays []config.DisplayConfig
	}{Displays: list})
	if err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}

func (kiosk *KioskWeb) displayReloadForm(w http.ResponseWriter, r *http.Request) {
	err := templates.ExecuteTemplate(w, "display_reload.html", struct {
	}{})
	if err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}

func (kiosk *KioskWeb) displayAddForm(w http.ResponseWriter, r *http.Request) {
	err := templates.ExecuteTemplate(w, "display_form.html", struct {
		Edit       bool
		DebugPort  int
		Name       string
		X, Y       int
		Fullscreen bool
		Exec       config.ExecConfig
	}{
		DebugPort:  kiosk.cfg.NextDebugPort(),
		Name:       kiosk.cfg.NextDisplayName(),
		X:          0,
		Y:          0,
		Fullscreen: false,
		Edit:       false,
		Exec: config.ExecConfig{
			Command:      "",
			Args:         []string{},
			WindowSearch: "",
		},
	})
	if err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}

func (kiosk *KioskWeb) displayEditForm(w http.ResponseWriter, r *http.Request) {
	displayName := r.URL.Query().Get("display")

	mu.Lock()
	defer mu.Unlock()

	idx := kiosk.cfg.IndexOfDisplay(displayName)
	if idx == -1 {
		log.Printf("Display not found: %s", displayName)
		http.Error(w, "Display not found", http.StatusNotFound)
		return
	}

	err := templates.ExecuteTemplate(w, "display_form.html", struct {
		Edit       bool
		DebugPort  int
		Name       string
		X, Y       int
		Fullscreen bool
		Exec       config.ExecConfig
	}{
		DebugPort:  kiosk.cfg.Displays[idx].DebugPort,
		Name:       kiosk.cfg.Displays[idx].Name,
		X:          kiosk.cfg.Displays[idx].X,
		Y:          kiosk.cfg.Displays[idx].Y,
		Fullscreen: kiosk.cfg.Displays[idx].Fullscreen,
		Edit:       true,
		Exec:       kiosk.cfg.Displays[idx].Exec,
	})
	if err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}

func (kiosk *KioskWeb) displayAdd(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := r.FormValue("Name")
	mu.Lock()
	idx := kiosk.cfg.IndexOfDisplay(name)
	if idx != -1 {
		http.Error(w, "Display already exists", http.StatusBadRequest)

		mu.Unlock()
		return
	}

	newDisplay := config.DisplayConfig{
		Name:       name,
		DebugPort:  parseFormInt(r, "DebugPort"),
		X:          parseFormInt(r, "X"),
		Y:          parseFormInt(r, "Y"),
		Fullscreen: r.FormValue("Fullscreen") == "true",
		Tabs:       []config.TabConfig{},
	}

	if kiosk.options.Parent != nil {
		if err := kiosk.options.Parent.AddDisplay(newDisplay); err != nil {
			log.Printf("Error adding display: %v", err)
		}
	}

	kiosk.cfg.Displays = append(kiosk.cfg.Displays, newDisplay)

	err := kiosk.cfg.Save(kiosk.options.ConfigFile)
	if err != nil {
		log.Printf("Error saving config: %v", err)
	}

	mu.Unlock()

	kiosk.getDisplayList(w, r)
}

func (kiosk *KioskWeb) displayEdit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := r.FormValue("Name")
	mu.Lock()

	idx := kiosk.cfg.IndexOfDisplay(name)
	if idx == -1 {
		log.Printf("Display not found: %s", name)
		http.Error(w, "Display not found", http.StatusNotFound)
		mu.Unlock()
		return
	}

	kiosk.cfg.Displays[idx].DebugPort = parseFormInt(r, "DebugPort")
	kiosk.cfg.Displays[idx].X = parseFormInt(r, "X")
	kiosk.cfg.Displays[idx].Y = parseFormInt(r, "Y")
	kiosk.cfg.Displays[idx].Fullscreen = r.FormValue("Fullscreen") == "true"
	kiosk.cfg.Displays[idx].Exec = config.ExecConfig{
		Command:             r.FormValue("Exec.Command"),
		Args:                r.Form["Exec.Args"],
		WindowSearch:        r.FormValue("Exec.WindowSearch"),
		DelayBeforeSendKeys: parseFormInt(r, "Exec.DelayBeforeSendKeys"),
		SendKeys:            r.Form["Exec.SendKeys"],
	}

	if kiosk.options.Parent != nil {
		if err := kiosk.options.Parent.EditDisplay(kiosk.cfg.Displays[idx]); err != nil {
			log.Printf("Error editing display: %v", err)
		}
	}

	err := kiosk.cfg.Save(kiosk.options.ConfigFile)
	if err != nil {
		log.Printf("Error saving config: %v", err)
	}

	mu.Unlock()

	kiosk.getDisplayList(w, r)
}

func (kiosk *KioskWeb) displayRemoveForm(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := r.FormValue("name")
	mu.Lock()
	idx := kiosk.cfg.IndexOfDisplay(name)
	mu.Unlock()

	if idx == -1 {
		log.Printf("Display not found: %s", name)
		http.Error(w, "Display not found", http.StatusNotFound)
		return
	}
	err := templates.ExecuteTemplate(w, "display_remove.html", struct {
		Name string
	}{
		Name: kiosk.cfg.Displays[idx].Name,
	})
	if err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}

func (kiosk *KioskWeb) displayRemove(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := r.FormValue("name")
	mu.Lock()

	idx := kiosk.cfg.IndexOfDisplay(name)
	if idx >= 0 {
		kiosk.cfg.Displays = append(kiosk.cfg.Displays[:idx], kiosk.cfg.Displays[idx+1:]...)
	}

	err := kiosk.cfg.Save(kiosk.options.ConfigFile)
	if err != nil {
		log.Printf("Error saving config: %v", err)
	}

	mu.Unlock()

	kiosk.getDisplayList(w, r)
}

func (kiosk *KioskWeb) tabAdd(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := r.FormValue("Display")
	mu.Lock()

	for i, d := range kiosk.cfg.Displays {
		if d.Name == name {
			kiosk.cfg.Displays[i].Tabs = append(kiosk.cfg.Displays[i].Tabs, config.TabConfig{
				URL:               r.FormValue("URL"),
				RefreshBeforeLoad: r.FormValue("RefreshBeforeLoad") == "true",
				RefreshAfterLoad:  r.FormValue("RefreshAfterLoad") == "true",
				RefreshInterval:   parseFormInt(r, "RefreshInterval"),
				DelayAfterRefresh: parseFormInt(r, "DelayAfterRefresh"),
				DwellTime:         parseFormInt(r, "DwellTime"),
			})
			break
		}
	}

	err := kiosk.cfg.Save(kiosk.options.ConfigFile)
	if err != nil {
		log.Printf("Error saving config: %v", err)
	}

	mu.Unlock()

	kiosk.getDisplayList(w, r)
}

func (kiosk *KioskWeb) tabForm(w http.ResponseWriter, r *http.Request) {
	displayName := r.URL.Query().Get("display")

	err := templates.ExecuteTemplate(w, "tab_form.html", struct {
		Display string
		Tab     *config.TabConfig
		Edit    bool
	}{
		Display: displayName,
		Tab: &config.TabConfig{
			URL:               "",
			RefreshBeforeLoad: false,
			RefreshAfterLoad:  false,
			RefreshInterval:   30,
			DelayAfterRefresh: 3,
			DwellTime:         kiosk.cfg.DwellTime,
		},
		Edit: true,
	})
	if err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}

func (kiosk *KioskWeb) tabEditForm(w http.ResponseWriter, r *http.Request) {
	displayName := r.URL.Query().Get("display")
	tabURL := r.URL.Query().Get("url")

	mu.Lock()
	defer mu.Unlock()

	var tab *config.TabConfig
	idx := kiosk.cfg.IndexOfDisplay(displayName)
	if idx == -1 {
		log.Printf("Display not found: %s", displayName)
		http.Error(w, "Display not found", http.StatusNotFound)
		return
	}

	for _, t := range kiosk.cfg.Displays[idx].Tabs {
		if t.URL == tabURL {
			tab = &t
			break
		}
	}

	if tab == nil {
		log.Printf("Tab not found for display %s with URL %s", displayName, tabURL)
		http.Error(w, "Tab not found", http.StatusNotFound)
		return
	}

	err := templates.ExecuteTemplate(w, "tab_form.html", struct {
		Display string
		Tab     *config.TabConfig
		Edit    bool
	}{
		Display: displayName,
		Tab:     tab,
		Edit:    true,
	})
	if err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}

func (kiosk *KioskWeb) tabEdit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	displayName := r.FormValue("Display")
	originalURL := r.FormValue("OriginalURL")

	newTab := config.TabConfig{
		URL:               r.FormValue("URL"),
		RefreshBeforeLoad: r.FormValue("RefreshBeforeLoad") == "true",
		RefreshAfterLoad:  r.FormValue("RefreshAfterLoad") == "true",
		RefreshInterval:   parseFormInt(r, "RefreshInterval"),
		DelayAfterRefresh: parseFormInt(r, "DelayAfterRefresh"),
		DwellTime:         parseFormInt(r, "DwellTime"),
	}

	mu.Lock()

	idx := kiosk.cfg.IndexOfDisplay(displayName)
	if idx == -1 {
		log.Printf("Display not found: %s", displayName)
		http.Error(w, "Display not found", http.StatusNotFound)

		mu.Unlock()
		return
	}

	found := false
	for i, t := range kiosk.cfg.Displays[idx].Tabs {
		if t.URL == originalURL {
			if kiosk.options.Parent != nil {
				if err := kiosk.options.Parent.EditTab(displayName, newTab); err != nil {
					log.Printf("Error editing tab: %v", err)
				}
			}

			kiosk.cfg.Displays[idx].Tabs[i] = newTab
			found = true
			break
		}
	}

	if !found {
		if kiosk.options.Parent != nil {
			if err := kiosk.options.Parent.AddTab(displayName, newTab); err != nil {
				log.Printf("Error adding tab: %v", err)
			}
		}

		kiosk.cfg.Displays[idx].Tabs = append(kiosk.cfg.Displays[idx].Tabs, newTab)
	}

	err := kiosk.cfg.Save(kiosk.options.ConfigFile)
	if err != nil {
		log.Printf("Error saving config: %v", err)
	}

	mu.Unlock()

	kiosk.getDisplayList(w, r)
}

func (kiosk *KioskWeb) tabRemoveForm(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	displayName := r.FormValue("display")
	tabURL := r.FormValue("url")
	mu.Lock()
	idx := kiosk.cfg.IndexOfDisplay(displayName)
	mu.Unlock()

	if idx == -1 {
		log.Printf("Display not found: %s", displayName)
		http.Error(w, "Display not found", http.StatusNotFound)
		return
	}
	err := templates.ExecuteTemplate(w, "tab_remove.html", struct {
		Display string
		URL     string
	}{
		Display: displayName,
		URL:     tabURL,
	})
	if err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}

func (kiosk *KioskWeb) tabRemove(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	displayName := r.FormValue("display")
	log.Printf("Removing %s", displayName)

	url := r.FormValue("url")
	mu.Lock()

	idx := kiosk.cfg.IndexOfDisplay(displayName)
	if idx == -1 {
		http.Error(w, "Display not found", http.StatusNotFound)
		mu.Unlock()
		return
	}

	for j, t := range kiosk.cfg.Displays[idx].Tabs {
		if t.URL == url {
			if kiosk.options.Parent != nil {
				if err := kiosk.options.Parent.RemoveTab(displayName, url); err != nil {
					log.Printf("Error removing tab: %v", err)
				}
			}

			kiosk.cfg.Displays[idx].Tabs = append(kiosk.cfg.Displays[idx].Tabs[:j], kiosk.cfg.Displays[idx].Tabs[j+1:]...)
			break
		}
	}

	err := kiosk.cfg.Save(kiosk.options.ConfigFile)
	if err != nil {
		log.Printf("Error saving config: %v", err)
	}

	mu.Unlock()

	kiosk.getDisplayList(w, r)
}

func (kiosk *KioskWeb) displayReloadConfirmed(w http.ResponseWriter, r *http.Request) {
	if kiosk.options.Parent != nil {
		if err := kiosk.options.Parent.ReloadDisplays(); err != nil {
			log.Printf("Error reloading displays: %v", err)
			http.Error(w, "Error reloading displays", http.StatusInternalServerError)
			return
		}
		log.Println("Displays reloaded successfully")
	}

	kiosk.getDisplayList(w, r)
}

// --- Middleware ---
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("[REQ] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
		log.Printf("[DONE] %s %s in %v", r.Method, r.URL.Path, time.Since(start))
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[PANIC] %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type KioskWebOptions struct {
	Addr       string
	ConfigFile string
	Parent     IKiosk
}

type KioskWeb struct {
	ctx     context.Context
	cancel  context.CancelFunc
	options KioskWebOptions
	cfg     config.Config
}

func NewKioskWeb(ctx context.Context, options KioskWebOptions) *KioskWeb {
	ctx, cancel := context.WithCancel(ctx)
	return &KioskWeb{
		ctx:     ctx,
		cancel:  cancel,
		options: options,
	}
}
func (kiosk *KioskWeb) Start() {
	err := config.Load(&kiosk.cfg, kiosk.options.ConfigFile)
	if err != nil {
		log.Fatal(err)
	}

	// templates = template.Must(template.ParseGlob("templates/*.html"))
	templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

	mux := http.NewServeMux()

	mux.HandleFunc("/", kiosk.index)
	mux.HandleFunc("/display/list", kiosk.getDisplayList)
	mux.HandleFunc("/display/reload", kiosk.displayReloadConfirmed)
	mux.HandleFunc("/display/reload-form", kiosk.displayReloadForm)
	mux.HandleFunc("/display/new-form", kiosk.displayAddForm)
	mux.HandleFunc("/display/edit-form", kiosk.displayEditForm)
	mux.HandleFunc("/display/add", kiosk.displayAdd)
	mux.HandleFunc("/display/edit", kiosk.displayEdit)
	mux.HandleFunc("/display/remove-form", kiosk.displayRemoveForm)
	mux.HandleFunc("/display/remove", kiosk.displayRemove)
	mux.HandleFunc("/tab/new-form", kiosk.tabForm)
	mux.HandleFunc("/tab/add", kiosk.tabAdd)
	mux.HandleFunc("/tab/remove-form", kiosk.tabRemoveForm)
	mux.HandleFunc("/tab/remove", kiosk.tabRemove)
	mux.HandleFunc("/tab/edit-form", kiosk.tabEditForm)
	mux.HandleFunc("/tab/edit", kiosk.tabEdit)

	// mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Wrap with middleware
	handler := loggingMiddleware(recoveryMiddleware(mux))

	log.Printf("Starting Kiosk web server on %s", kiosk.options.Addr)
	log.Fatal(http.ListenAndServe(kiosk.options.Addr, handler))
}

func (kiosk *KioskWeb) Stop() {
	kiosk.cancel()
	log.Println("Kiosk web server stopped")
}
