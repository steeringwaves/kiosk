package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"kiosk/internal/config"
	"kiosk/internal/web"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type TabState struct {
	config.TabConfig
	ID          string
	LastRefresh int64
	WSURL       string
	WSConn      *websocket.Conn
}

type DisplayState struct {
	Config    config.DisplayConfig
	DebugPort int
	Tabs      []*TabState
	WindowID  string
}

type RequestID struct {
	mu sync.Mutex
	id int
}

func (r *RequestID) Next() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.id
	r.id++
	return id
}

type Kiosk struct {
	requestID   *RequestID
	cfg         config.Config
	cfgFilename string

	mu      sync.Mutex
	windows map[string]*DisplayState
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewKiosk() *Kiosk {
	return &Kiosk{
		requestID: &RequestID{id: 1},
		windows:   make(map[string]*DisplayState),
	}
}

func binPresent(bin string) bool {
	if _, err := exec.LookPath(bin); err != nil {
		return false
	}

	return true
}

func ensureDeps(bins []string) {
	for _, b := range bins {
		if _, err := exec.LookPath(b); err != nil {
			log.Fatalf("Missing dependency: %s", b)
		}
	}
}

func ctxHandler(ctx context.Context, cancel context.CancelFunc) {
	signals := []os.Signal{syscall.SIGINT, syscall.SIGTERM}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, signals...)

	go func() {
		defer cancel()

		select {
		case <-sigs:
			cancel()
			return
		case <-ctx.Done():
			return
		}
	}()
}

type KioskWeb struct {
	ctx    context.Context
	Parent *Kiosk
}

func (e *KioskWeb) AddDisplay(display config.DisplayConfig) error {
	log.Printf("Adding display: %+v", display)
	return nil
}

func (e *KioskWeb) RemoveDisplay(name string) error {
	log.Printf("Removing display: %s", name)
	return nil
}

func (e *KioskWeb) AddTab(displayName string, tab config.TabConfig) error {
	log.Printf("Adding tab to display %s: %+v", displayName, tab)
	return nil
}

func (e *KioskWeb) RemoveTab(displayName, tabURL string) error {
	log.Printf("Removing tab from display %s: %s", displayName, tabURL)
	return nil
}

func (e *KioskWeb) EditDisplay(display config.DisplayConfig) error {
	log.Printf("Editing display: %+v", display)
	return nil
}

func (e *KioskWeb) EditTab(displayName string, tab config.TabConfig) error {
	log.Printf("Editing tab on display %s: %+v", displayName, tab)
	return nil
}

func (e *KioskWeb) ReloadDisplays() error {
	log.Println("Reloading displays")

	go func() {
		e.Parent.Stop()
		time.Sleep(1 * time.Second)

		e.Parent.loadConfig()
		e.Parent.Run(e.ctx)

		time.Sleep(1 * time.Second)
		// os.Exit(0)
	}()

	return nil
}

func main() {
	ensureDeps([]string{"xdotool", "chromium"})

	kiosk := NewKiosk()
	kiosk.loadConfig()

	ctx, cancel := context.WithCancel(context.Background())
	ctxHandler(ctx, cancel)

	// Start the web UI
	portStr := os.Getenv("PORT")
	if portStr == "" {
		portStr = "8080"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatalf("Invalid PORT: %s", portStr)
	}

	go func() {
		options := web.KioskWebOptions{
			Addr:       fmt.Sprintf(":%d", port),
			ConfigFile: kiosk.cfgFilename,
			Parent: &KioskWeb{
				ctx:    ctx,
				Parent: kiosk,
			},
		}
		kioskWeb := web.NewKioskWeb(ctx, options)
		kioskWeb.Start()
	}()

	// Start the kiosk
	kiosk.Run(ctx)

	<-ctx.Done()
	go func() {
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()

	kiosk.Stop()
}

func (kiosk *Kiosk) Run(ctx context.Context) error {
	kiosk.ctx, kiosk.cancel = context.WithCancel(ctx)

	defer func() {
		for _, display := range kiosk.cfg.Displays {
			kiosk.CloseWindow(display.Name)
		}
	}()

	for _, display := range kiosk.cfg.Displays {
		if display.Exec.Command != "" {
			kiosk.launchCustom(display.Name)
			if kiosk.ctx.Err() != nil {
				return kiosk.ctx.Err()
			}
			continue
		}

		if len(display.Tabs) == 0 {
			continue
		}

		kiosk.launchChrome(display.Name)
		if kiosk.ctx.Err() != nil {
			return kiosk.ctx.Err()
		}
	}

	for _, display := range kiosk.cfg.Displays {
		kiosk.moveWindow(display.Name)
		if kiosk.ctx.Err() != nil {
			return kiosk.ctx.Err()
		}
	}

	for _, display := range kiosk.cfg.Displays {
		if display.Fullscreen {
			kiosk.sendFullscreen(display.Name)
			if kiosk.ctx.Err() != nil {
				return kiosk.ctx.Err()
			}
		}

		for _, key := range display.Exec.SendKeys {
			if key == "" {
				continue
			}

			delay := time.Duration(display.Exec.DelayBeforeSendKeys) * time.Second
			kiosk.SendKeyToWindow(display.Name, key, delay)
			if kiosk.ctx.Err() != nil {
				return kiosk.ctx.Err()
			}
		}
	}

	for _, display := range kiosk.cfg.Displays {
		if display.Exec.Command != "" {
			kiosk.execCycle(display.Name)
			continue
		}

		if len(display.Tabs) == 0 {
			continue
		}

		kiosk.tabCycler(display.Name)
		if kiosk.ctx.Err() != nil {
			return kiosk.ctx.Err()
		}
	}

	kiosk.wg.Wait()

	kiosk.mu.Lock()
	if kiosk.cancel != nil {
		kiosk.cancel()
		kiosk.cancel = nil
	}

	kiosk.mu.Unlock()

	return nil
}

func (kiosk *Kiosk) Stop() {
	kiosk.mu.Lock()

	if kiosk.cancel != nil {
		kiosk.cancel()
		kiosk.cancel = nil
		kiosk.mu.Unlock()

		kiosk.wg.Wait()
		return
	}

	kiosk.mu.Unlock()
}

func (kiosk *Kiosk) loadConfig() {
	kiosk.mu.Lock()
	defer kiosk.mu.Unlock()

	kiosk.cfgFilename = os.Getenv("CONFIG_FILE")
	if kiosk.cfgFilename == "" {
		kiosk.cfgFilename = "./kiosk.yml"
	}

	err := config.Load(&kiosk.cfg, kiosk.cfgFilename)
	if err != nil {
		log.Fatal(err)
	}

	kiosk.windows = make(map[string]*DisplayState)

	for _, display := range kiosk.cfg.Displays {
		if _, exists := kiosk.windows[display.Name]; !exists {
			ds := &DisplayState{
				Config:    display,
				DebugPort: display.DebugPort,
				Tabs:      make([]*TabState, len(display.Tabs)),
			}

			for i, tab := range display.Tabs {
				ds.Tabs[i] = &TabState{
					TabConfig:   tab,
					ID:          "",
					LastRefresh: time.Now().Unix(),
					WSURL:       "",
					WSConn:      nil,
				}
			}

			kiosk.windows[display.Name] = ds
		}
	}
}

func (kiosk *Kiosk) xdotoolSearchVisible(searchName string) ([]string, error) {
	if searchName == "" {
		searchName = ".*" // Default to all visible windows
	}

	out, err := exec.Command("xdotool", "search", "--onlyvisible", "--name", searchName).Output()
	if err != nil {
		return []string{}, nil
	}

	// Split the output into window IDs
	winIDs := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(winIDs) == 0 || winIDs[0] == "" {
		return []string{}, nil
	}

	// Find a unique ID that is not already in use
	result := make([]string, 0)
	for _, id := range winIDs {
		if id == "" {
			continue
		}

		// ensure the ID is a number
		if _, err := strconv.Atoi(id); err != nil {
			continue
		}

		result = append(result, id)
	}

	return result, nil
}

func (kiosk *Kiosk) xdotoolFindLatestWindowID(name string, beforeExecIDs []string, afterExecIDs []string) (string, error) {
	if len(afterExecIDs) == 0 {
		return "", fmt.Errorf("[%s] No visible windows found", name)
	}

	// Filter out IDs that were already in use before the exec
	winIDs := []string{}
	for _, id := range afterExecIDs {
		found := false
		for _, beforeID := range beforeExecIDs {
			if id == beforeID {
				found = true
				break
			}
		}
		if !found {
			winIDs = append(winIDs, id)
		}
	}

	if len(winIDs) == 0 {
		return "", fmt.Errorf("[%s] No new window found after exec", name)
	}

	// Gather existing IDs
	existing := make(map[string]bool)
	for _, w := range kiosk.windows {
		if w.WindowID != "" {
			existing[w.WindowID] = true
		}
	}

	// Find a unique ID that is not already in use
	for _, id := range winIDs {
		if !existing[id] {
			log.Printf("[%s] Opened window %s", name, id)
			return id, nil
		}
	}

	return winIDs[len(winIDs)-1], nil
}

func (kiosk *Kiosk) getLatestWindowID(name string, searchName string) (string, error) {
	if searchName == "" {
		searchName = "chromium"
	}

	out, err := exec.Command("xdotool", "search", "--onlyvisible", "--name", searchName).Output()
	if err != nil {
		return "", fmt.Errorf("[%s] Could not find window: %w", name, err)
	}

	// Split the output into window IDs
	winIDs := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(winIDs) == 0 || winIDs[0] == "" {
		return "", fmt.Errorf("[%s] No window found", name)
	}

	// Gather existing IDs
	existing := make(map[string]bool)
	for _, w := range kiosk.windows {
		if w.WindowID != "" {
			existing[w.WindowID] = true
		}
	}

	// Find a unique ID that is not already in use
	for _, id := range winIDs {
		exists := existing[id]
		if !exists {
			log.Printf("[%s] Opened window %s", name, id)
			return id, nil
		}
	}

	return "", fmt.Errorf("[%s] No unique window ID found", name)
}

func (kiosk *Kiosk) launchCustom(name string) {
	kiosk.mu.Lock()
	defer kiosk.mu.Unlock()

	window, ok := kiosk.windows[name]
	if !ok {
		return
	}

	originalWinIDs, err := kiosk.xdotoolSearchVisible(window.Config.Exec.WindowSearch)
	if err != nil {
		log.Printf("[%s] Error searching for visible windows: %v", name, err)
		return
	}

	log.Printf("[%s] Launching custom command: %s with args: %v", name, window.Config.Exec.Command, window.Config.Exec.Args)

	cmd := exec.CommandContext(kiosk.ctx, window.Config.Exec.Command, window.Config.Exec.Args...)
	cmd.Stdout = nil
	err = cmd.Start()
	if err != nil {
		log.Printf("[%s] Error starting command: %v", name, err)
		return
	}

	firstRun := true
	for {
		if !firstRun {
			select {
			case <-time.After(time.Second):
			case <-kiosk.ctx.Done():
				return
			}
		}
		firstRun = false

		winIDs, err := kiosk.xdotoolSearchVisible(window.Config.Exec.WindowSearch)
		if err != nil {
			log.Printf("[%s] Error searching for visible windows: %v", name, err)
			continue
		}

		if len(winIDs) == 0 {
			log.Printf("[%s] No visible windows found for %s", name, window.Config.Exec.WindowSearch)
			continue
		}

		winID, err := kiosk.xdotoolFindLatestWindowID(name, originalWinIDs, winIDs)
		if err != nil {
			log.Printf("[%s] Error finding latest window ID: %v", name, err)
			continue
		}

		window.WindowID = winID
		break
	}
}

func chromiumUserDataDir(name string) string {
	return fmt.Sprintf("/tmp/.kiosk-chrome-user-data-%s", name)
}

func (kiosk *Kiosk) launchChrome(name string) {
	kiosk.mu.Lock()
	defer kiosk.mu.Unlock()

	window, ok := kiosk.windows[name]
	if !ok {
		return
	}

	port := window.DebugPort
	if port == 0 {
		port = kiosk.cfg.DebugPort
	}
	userDir := chromiumUserDataDir(name)
	os.RemoveAll(userDir)
	os.MkdirAll(userDir, 0755)

	if !kiosk.portAvailable(port) {
		log.Fatalf("[%s] Port %d in use", name, port)
	}

	url := window.Tabs[0].URL
	args := []string{
		fmt.Sprintf("--user-data-dir=%s", userDir),
		fmt.Sprintf("--window-size=%s", kiosk.cfg.NewWindowSize),
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--remote-allow-origins=http://localhost:%d", port),
		"--disable-session-crashed-bubble",
		"--disable-session-restore",
		"--disable-infobars",
		"--no-default-browser-check",
		"--no-first-run",
		"--disable-extensions",
		"--new-window",
		url,
	}

	originalWinIDs, err := kiosk.xdotoolSearchVisible("chromium")
	if err != nil {
		log.Printf("[%s] Error searching for visible windows: %v", name, err)
		return
	}

	cmd := exec.CommandContext(kiosk.ctx, "chromium", args...)
	cmd.Stderr = nil
	_ = cmd.Start()

	err = kiosk.waitForDebugger(name, port)
	if err != nil {
		log.Fatalf("[%s] Failed to wait for debugger: %v", name, err)
	}

	firstRun := true
	for {
		if !firstRun {
			select {
			case <-time.After(250 * time.Millisecond):
			case <-kiosk.ctx.Done():
				return
			}
			firstRun = false
		}

		winIDs, err := kiosk.xdotoolSearchVisible("chromium")
		if err != nil {
			log.Printf("[%s] Error searching for visible windows: %v", name, err)
			continue
		}

		if len(winIDs) == 0 {
			log.Printf("[%s] No visible windows found for %s", name, "chromium")
			continue
		}

		winID, err := kiosk.xdotoolFindLatestWindowID(name, originalWinIDs, winIDs)
		if err != nil {
			log.Printf("[%s] Error finding latest window ID: %v", name, err)
			continue
		}

		window.WindowID = winID
		break
	}

	// Fetch tabs
	err = kiosk.waitForTabID(name, port, window, 0)
	if err != nil {
		log.Fatalf("[%s] Failed to wait for tab ID: %v", name, err)
	}

	for i := 1; i < len(window.Tabs); i++ {
		tab := window.Tabs[i]

		args = []string{
			"--new-tab",
			tab.URL,
			fmt.Sprintf("--user-data-dir=%s", userDir),
		}

		cmd = exec.CommandContext(kiosk.ctx, "chromium", args...)
		cmd.Stderr = nil
		_ = cmd.Start()

		err = kiosk.waitForTabID(name, port, window, i)
		if err != nil {
			log.Fatalf("[%s] Failed to wait for tab ID %d: %v", name, i, err)
		}
	}
}

func (kiosk *Kiosk) waitForTabID(name string, port int, window *DisplayState, tabIndex int) error {
	for {
		// Fetch tabs
		chromeTabs, err := kiosk.fetchTabs(port)
		if err != nil || len(chromeTabs) == 0 {
			log.Printf("[%s] Failed to fetch tabs\n", name)

			select {
			case <-time.After(1 * time.Second):
			case <-kiosk.ctx.Done():
				return kiosk.ctx.Err()
			}
			continue
		}

		// Store state
		for _, chromeTab := range chromeTabs {
			exists := false
			id, ok := chromeTab["id"].(string)
			if !ok {
				continue
			}

			wsURL, ok := chromeTab["webSocketDebuggerUrl"].(string)
			if !ok {
				continue
			}

			for _, t := range window.Tabs {
				if t.ID != "" && t.ID == id {
					exists = true
					break
				}
			}

			if exists {
				continue
			}

			window.Tabs[tabIndex].ID = id
			window.Tabs[tabIndex].WSURL = wsURL
			log.Printf("[%s] Tab: %s (ID: %s, WS: %s)\n", name, window.Tabs[tabIndex].URL, id, wsURL)
			return nil
		}

		select {
		case <-time.After(1 * time.Second):
		case <-kiosk.ctx.Done():
			return kiosk.ctx.Err()
		}
	}
}

func (kiosk *Kiosk) waitForDebugger(name string, port int) error {
	for {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/json", port))
		if err == nil && resp.StatusCode == 200 {
			return nil
		}
		select {
		case <-time.After(1 * time.Second):
		case <-kiosk.ctx.Done():
			return kiosk.ctx.Err()
		}
	}
}

func (kiosk *Kiosk) fetchTabs(port int) ([]map[string]interface{}, error) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/json", port))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tabs []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tabs); err != nil {
		return nil, err
	}
	return tabs, nil
}

func (kiosk *Kiosk) moveWindow(name string) {
	window, ok := kiosk.windows[name]
	if !ok {
		log.Printf("[%s] No window state found for %s", name, name)
		return
	}

	log.Printf("[%s] Activating window %s\n", name, window.WindowID)
	err := exec.CommandContext(kiosk.ctx, "xdotool", "windowactivate", window.WindowID).Run()
	if err != nil {
		log.Printf("[%s] Error activating window %s: %v", name, window.WindowID, err)
	}

	x, y := strconv.Itoa(window.Config.X), strconv.Itoa(window.Config.Y)

	log.Printf("[%s] Moving window %s to %s:%s\n", name, window.WindowID, x, y)
	err = exec.CommandContext(kiosk.ctx, "xdotool", "windowmove", window.WindowID, x, y).Run()
	if err != nil {
		log.Printf("[%s] Error moving window %s: %v", name, window.WindowID, err)
	}
}

func (kiosk *Kiosk) SendKeyToWindow(name string, key string, delayBeforeSending time.Duration) {
	window, ok := kiosk.windows[name]
	if !ok {
		log.Printf("[%s] No window state found for %s", name, name)
		return
	}

	select {
	case <-time.After(delayBeforeSending):
	case <-kiosk.ctx.Done():
		return
	}

	log.Printf("[%s] Activating window %s\n", name, window.WindowID)
	err := exec.CommandContext(kiosk.ctx, "xdotool", "windowactivate", window.WindowID).Run()
	if err != nil {
		log.Printf("[%s] Error activating window %s: %v", name, window.WindowID, err)
	}

	log.Printf("[%s] Sending %s to window %s\n", name, key, window.WindowID)
	err = exec.CommandContext(kiosk.ctx, "xdotool", "key", "--window", window.WindowID, key).Run()
	if err != nil {
		log.Printf("[%s] Error sending %s to window %s: %v", name, key, window.WindowID, err)
	}
}

func (kiosk *Kiosk) CloseWindow(name string) {
	window, ok := kiosk.windows[name]
	if !ok {
		log.Printf("[%s] No window state found for %s", name, name)
		return
	}

	log.Printf("[%s] Closing window %s\n", name, window.WindowID)
	err := exec.Command("xdotool", "windowclose", window.WindowID).Run()
	if err != nil {
		log.Printf("[%s] Error closing window %s: %v", name, window.WindowID, err)
	}
}

func (kiosk *Kiosk) sendFullscreen(name string) {
	kiosk.SendKeyToWindow(name, "F11", time.Second)
}

func (kiosk *Kiosk) portAvailable(port int) bool {
	if !binPresent("lsof") {
		log.Printf("lsof not found, assuming port %d is available", port)
		return true
	}

	cmd := exec.Command("lsof", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-Pn")
	return cmd.Run() != nil
}

func (kiosk *Kiosk) refreshTabAndWait(tab *TabState, name string) (bool, error) {
	log.Printf("[%s] Refreshing tab %s\n", name, tab.URL)
	err := kiosk.navigateChromeTab(*tab)
	if err != nil {
		log.Printf("[%s] Error refreshing tab %s: %v", name, tab.ID, err)
		return false, err
	}

	if tab.DelayAfterRefresh > 0 {
		select {
		case <-time.After(time.Duration(tab.DelayAfterRefresh) * time.Second):
		case <-kiosk.ctx.Done():
			return false, kiosk.ctx.Err()
		}
	}

	tab.LastRefresh = time.Now().Unix()
	log.Printf("[%s] Tab %s refreshed successfully", name, tab.URL)
	return true, nil
}

func (kiosk *Kiosk) execCycle(name string) {
	kiosk.mu.Lock()
	_, ok := kiosk.windows[name]
	kiosk.mu.Unlock()

	if !ok {
		return
	}

	kiosk.wg.Add(1)
	go func() {
		defer func() {
			kiosk.wg.Done()
		}()

		<-kiosk.ctx.Done()
	}()
}

func (kiosk *Kiosk) tabCycler(name string) {
	kiosk.mu.Lock()
	display, ok := kiosk.windows[name]
	kiosk.mu.Unlock()

	if !ok {
		return
	}

	kiosk.wg.Add(1)
	go func() {
		defer func() {
			userDir := chromiumUserDataDir(name)
			os.RemoveAll(userDir)
			kiosk.wg.Done()
		}()

		for {
			for _, tab := range display.Tabs {
				dwell := time.Duration(tab.DwellTime) * time.Second

				refreshed := false

				if tab.RefreshInterval > 0 && time.Since(time.Unix(tab.LastRefresh, 0)) > time.Duration(tab.RefreshInterval)*time.Second {
					refreshed, _ = kiosk.refreshTabAndWait(tab, name)
				}

				if !refreshed && tab.RefreshBeforeLoad {
					refreshed, _ = kiosk.refreshTabAndWait(tab, name)
				}

				log.Printf("[%s] Activating tab %s for %v seconds", name, tab.URL, dwell.Seconds())
				err := kiosk.activateChromeTab(display.DebugPort, tab.ID)
				if err != nil {
					log.Printf("[%s] Error activating tab %s: %v", name, tab.ID, err)
				}

				if !refreshed && tab.RefreshAfterLoad {
					kiosk.refreshTabAndWait(tab, name)
				}

				select {
				case <-time.After(dwell):
				case <-kiosk.ctx.Done():
					return
				}
			}
		}
	}()
}

func (kiosk *Kiosk) activateChromeTab(port int, tabID string) error {
	_, err := http.Get(fmt.Sprintf("http://localhost:%d/json/activate/%s", port, tabID))
	return err
}

func (kiosk *Kiosk) refreshChromeTab(tab TabState) error {
	requestID := kiosk.requestID.Next()

	req := map[string]interface{}{
		"id":     requestID,
		"method": "Page.reload",
		"params": map[string]interface{}{"ignoreCache": true},
	}
	return kiosk.chromeWebsocketSend(tab, req)
}

func (kiosk *Kiosk) navigateChromeTab(tab TabState) error {
	requestID := kiosk.requestID.Next()

	req := map[string]interface{}{
		"id":     requestID,
		"method": "Page.navigate",
		"params": map[string]interface{}{"url": tab.URL, "ignoreCache": true},
	}
	return kiosk.chromeWebsocketSend(tab, req)
}

func (kiosk *Kiosk) chromeWebsocketSend(tab TabState, req map[string]interface{}) error {
	if tab.WSURL == "" {
		return errors.New("no websocket url")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, tab.WSURL, nil)
	if err != nil {
		return err
	}
	defer c.Close(websocket.StatusNormalClosure, "bye")

	return wsjson.Write(ctx, c, req)
}
