package main

import (
	"context"
	"log"
	"os"

	"kiosk/internal/config"
	"kiosk/internal/web"
)

type Example struct{}

func (e *Example) AddDisplay(display config.DisplayConfig) error {
	log.Printf("Adding display: %+v", display)
	return nil
}
func (e *Example) RemoveDisplay(name string) error {
	log.Printf("Removing display: %s", name)
	return nil
}
func (e *Example) AddTab(displayName string, tab config.TabConfig) error {
	log.Printf("Adding tab to display %s: %+v", displayName, tab)
	return nil
}
func (e *Example) RemoveTab(displayName, tabURL string) error {
	log.Printf("Removing tab from display %s: %s", displayName, tabURL)
	return nil
}
func (e *Example) EditDisplay(display config.DisplayConfig) error {
	log.Printf("Editing display: %+v", display)
	return nil
}
func (e *Example) EditTab(displayName string, tab config.TabConfig) error {
	log.Printf("Editing tab on display %s: %+v", displayName, tab)
	return nil
}
func (e *Example) ReloadDisplays() error {
	log.Println("Reloading displays")
	return nil
}

func main() {
	file := os.Getenv("CONFIG_FILE")
	if file == "" {
		file = "./kiosk.yml"
	}
	ctx := context.Background()
	options := web.KioskWebOptions{
		Addr:       ":8080",
		ConfigFile: file,
		Parent:     &Example{},
	}
	kioskWeb := web.NewKioskWeb(ctx, options)
	kioskWeb.Start()
	<-ctx.Done()
}
