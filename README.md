# chromium kiosk

## deps

```sh
sudo apt install xdotool
```

### build

```sh
# compile app using docker
make release

# compile app using locally install go version
make kiosk
```

## usage

```sh
export CONFIG_FILE=config.yml

# disable chromium warnings on startup
sed -i 's/"exited_cleanly":false/"exited_cleanly":true/' ~/.config/chromium/'Local State'
sed -i 's/"exited_cleanly":false/"exited_cleanly":true/; s/"exit_type":"[^"]\+"/"exit_type":"Normal"/' ~/.config/chromium/Default/Preferences

sed -i 's/"exited_cleanly":false/"exited_cleanly":true/' ~/.config/google-chrome/'Local State'
sed -i 's/"exited_cleanly":false/"exited_cleanly":true/; s/"exit_type":"[^"]\+"/"exit_type":"Normal"/' ~/.config/google-chrome/Default/Preferences

sed -i 's/"exited_cleanly": false/"exited_cleanly": true/' ~/snap/chromium/common/chromium/'Local State'
sed -i 's/"exited_cleanly":false/"exited_cleanly":true/; s/"exit_type":"[^"]\+"/"exit_type":"Normal"/' ~/snap/chromium/common/chromium/Default/Preferences

./bin/kiosk

```
