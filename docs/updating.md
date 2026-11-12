# Phoenix updates

Phoenix uses Wails v3's updater with a signed update manifest hosted as a GitHub Release asset. The app checks only when the user selects **Check for updates**. It downloads and verifies an update only after **Download and install**, then applies it after **Restart to update**.

## Release signing key

The private Ed25519 key is kept outside this repository at `%LOCALAPPDATA%\Phoenix\update-signing\updater.key`. Back it up in a password manager or secure backup and add its complete PEM contents to the GitHub repository as the `PHOENIX_UPDATE_PRIVATE_KEY` Actions secret. Never commit or paste the private key into source files or logs. Phoenix embeds only `internal/update/updater-public.pem`.

Losing the private key means existing Phoenix installs can no longer verify new updates. Changing the public key later requires users to install a release signed by the old key first.

## Publishing

Push a version tag such as `v0.2.2` after the signing secret is configured. The Windows workflow builds a per-user NSIS installer, the raw executable used by the self-updater, and a signed `phoenix-update-manifest.json`, then verifies the manifest before publishing all three assets.

The published `v0.1.0` build predates this updater, so install `v0.2.1` manually with its per-user installer. That version and later releases can self-update.

The updater replaces the running executable in its installation directory. Phoenix installers therefore default to per-user scope under `%LOCALAPPDATA%\Programs\Phoenix`; machine installs under Program Files are not writable for ordinary users. Existing machine-scope installations must be reinstalled with the per-user installer before they can self-update.

## Local release checks

Use the same commands as CI after building `dist/phoenix-windows-amd64.exe`:

```powershell
$version = '0.2.1'
$key = "$env:LOCALAPPDATA\Phoenix\update-signing\updater.key"
$prefix = 'https://github.com/wahajnintyeight/project-phoenix-desktop-agent/releases/latest/download/'
wails3 updater manifest -version $version -channel stable -name "Phoenix $version" -key $key -url-prefix $prefix -output dist/phoenix-update-manifest.json dist/phoenix-windows-amd64.exe
wails3 updater verify -manifest dist/phoenix-update-manifest.json -publickey internal/update/updater-public.pem -dir dist
```
