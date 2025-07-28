# Chromium Kiosk Manager

A Linux-only kiosk manager that launches and positions Chromium windows across multiple displays using `xdotool`, supports tab cycling, and serves a live configuration UI over HTTP.

This tool is ideal for public information screens, wallboards, and other unattended display setups.

---

## Features

- Launches Chromium windows per display
- Moves and resizes them using `xdotool`
- Optionally fullscreens windows by issuing the `F11` key
- Cycles through tabs per display, with configurable dwell times
- Periodically refreshes pages with optional pre/post reload actions
- Live web UI to edit config (Changes require closing/reopening all chromium instances for now which is also available via the web UI)

---

## Requirements

- Linux with **X11** (Wayland is **not supported**)
- [`chromium`](https://www.chromium.org/)
- [`xdotool`](https://github.com/jordansissel/xdotool)

Install them on Debian/Ubuntu:

```bash
sudo apt install chromium xdotool
```

## Environment Variables

- `CONFIG_FILE` Path to .yml or .json configuration file
- `PORT` Web UI listen port (default is 8080 if unset)

## Configuration Fields

```yml
dwellTime: 30
debugPort: 0
newWindowSize: 1024,768
displays:
    - name: Display0
      debugPort: 9300
      x: 90
      y: 200
      fullscreen: false
      tabs:
        - url: google.com
          refreshBeforeLoad: false
          refreshAfterLoad: false
          refreshInterval: 30
          delayAfterRefresh: 3
          dwellTime: 5
        - url: duckduckgo.com
          refreshBeforeLoad: false
          refreshAfterLoad: false
          refreshInterval: 30
          delayAfterRefresh: 3
          dwellTime: 5
    - name: Display1
      debugPort: 9301
      x: 1200
      y: 600
      fullscreen: true
      tabs:
        - url: https://www.wpc.ncep.noaa.gov//noaa/noaa.gif
          refreshBeforeLoad: false
          refreshAfterLoad: false
          refreshInterval: 600
          delayAfterRefresh: 0
          dwellTime: 30
        - url: https://www.wpc.ncep.noaa.gov/basicwx/day0-7loop.html
          refreshBeforeLoad: false
          refreshAfterLoad: false
          refreshInterval: 600
          delayAfterRefresh: 0
          dwellTime: 30
        - url: https://time.gov
          refreshBeforeLoad: false
          refreshAfterLoad: false
          refreshInterval: 600
          delayAfterRefresh: 0
          dwellTime: 30
```

### Top-level

- dwellTime: Default seconds to show each tab before switching (can be overridden per-tab)
- debugPort: Default Chromium remote debugging port (0 means 9302)
- newWindowSize: Default window size as "width,height" string for non-fullscreen windows

### displays[]

- name: Logical name of the display (must be unique)
- debugPort: Remote debug port (required and must be unique per display)
- x, y: X/Y position of the Chromium window
- fullscreen: If true, launches window in fullscreen mode
- tabs[]: List of tabs to cycle through

### tabs[]

- url: Web address to load
- refreshBeforeLoad: Whether to refresh before activating this tab
- refreshAfterLoad: Whether to refresh after activating this tab
- refreshInterval: Seconds between auto-refreshes before (0 = disable) **NOTE: The refresh will happen prior to activating tab with this method**
- delayAfterRefresh: Seconds to wait after refreshing before activating this tab
- dwellTime: Optional override of top-level dwell time

## Building locally

```sh
# compile app using docker
make release

# compile app using locally install go version
make kiosk
```

## Usage

```sh
export CONFIG_FILE=config.yml
export PORT=8080

# disable chromium warnings on startup
sed -i 's/"exited_cleanly":false/"exited_cleanly":true/' ~/.config/chromium/'Local State'
sed -i 's/"exited_cleanly":false/"exited_cleanly":true/; s/"exit_type":"[^"]\+"/"exit_type":"Normal"/' ~/.config/chromium/Default/Preferences

sed -i 's/"exited_cleanly":false/"exited_cleanly":true/' ~/.config/google-chrome/'Local State'
sed -i 's/"exited_cleanly":false/"exited_cleanly":true/; s/"exit_type":"[^"]\+"/"exit_type":"Normal"/' ~/.config/google-chrome/Default/Preferences

sed -i 's/"exited_cleanly": false/"exited_cleanly": true/' ~/snap/chromium/common/chromium/'Local State'
sed -i 's/"exited_cleanly":false/"exited_cleanly":true/; s/"exit_type":"[^"]\+"/"exit_type":"Normal"/' ~/snap/chromium/common/chromium/Default/Preferences

./bin/kiosk

```
