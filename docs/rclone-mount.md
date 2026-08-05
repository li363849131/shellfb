# Mounting cloud storage with rclone

shellfb's preview panel and the integrated FileBrowser both operate within `/root` by default. Mounting cloud storage (Google Drive, OneDrive, S3, etc.) under `/root` makes it visible to both features without code changes.

This guide covers the systemd-based mount infrastructure included in this repository.

## Overview

- **Preview panel** (`/api/files`, `/api/download`) is rooted at `transfer.dir` (default `/root`)
- **FileBrowser** (网盘) is rooted at its configured `Root` (typically `/root`)
- Mounting at `/root/gdrive`, `/root/s3backup`, etc. makes those locations appear as folders in both UIs

## What's included

| File | Purpose |
|---|---|
| `systemd/rclone-mount@.service` | Template unit for mounting one remote |
| `systemd/rclone-mounts/*.env.example` | Per-instance configuration examples |
| `scripts/gdrive-setup` | Helper script for Google Drive OAuth setup |
| `scripts/check-rclone-mounts` | Health check for active mounts |

## Quick start (Google Drive)

### 1. Install rclone

```bash
# Official binary (recommended over distro packages)
curl -O https://downloads.rclone.org/rclone-current-linux-amd64.zip
unzip rclone-current-linux-amd64.zip
sudo install -m 755 rclone-*/rclone /usr/local/bin/rclone
```

### 2. Get an OAuth token

On a machine **with a browser** (your laptop, not the server), run:

```bash
rclone authorize "drive"
```

A browser window opens for Google login. After authorizing, rclone prints:

```
Paste the following into your remote machine --->
{"access_token":"ya29...","token_type":"Bearer","refresh_token":"1//0g...","expiry":"..."}
<---End paste
```

Copy the entire JSON blob (including the braces).

### 3. Configure the mount on the server

```bash
# Copy the template
sudo cp systemd/rclone-mounts/gdrive.env.example /etc/rclone-mounts/gdrive1.env

# Edit it — set REMOTE=gd1:/ and MOUNTPOINT=/root/gdrive1
sudo nano /etc/rclone-mounts/gdrive1.env

# Install the systemd unit
sudo cp systemd/rclone-mount@.service /etc/systemd/system/
sudo systemctl daemon-reload

# Create mount point and protect it while unmounted
sudo mkdir -p /root/gdrive1
sudo chattr +i /root/gdrive1

# Run the setup script with your token
sudo bash scripts/gdrive-setup gd1 '<paste-the-token-json-here>'
```

The script will:
- Create the rclone remote `gd1`
- Verify credentials against Google
- Start and enable the mount
- Run a health check

### 4. Verify

```bash
ls /root/gdrive1/
sudo /usr/local/bin/check-rclone-mounts gdrive1
```

The folder should now appear in both the preview panel and FileBrowser.

## Configuration reference

Each mount instance has a `.env` file in `/etc/rclone-mounts/`. Key settings:

| Variable | Purpose | Example |
|---|---|---|
| `REMOTE` | rclone remote name + path | `gd1:/` or `gd1:/Documents` |
| `MOUNTPOINT` | Where to mount (must be under `/root`) | `/root/gdrive1` |
| `VFS_CACHE_MODE` | `off`, `minimal`, `writes`, `full` | `full` (needed for random read/write) |
| `VFS_CACHE_MAX_SIZE` | Disk cache limit per mount | `2G` |
| `DIR_CACHE_TIME` | How long to cache directory listings | `30s` |
| `POLL_INTERVAL` | How often to poll remote for changes | `15s` |
| `EXTRA_FLAGS` | Additional rclone mount flags | `--read-only` |

For Google Drive specifically:
- `--drive-use-trash` defaults to `true` — deletes go to trash, not permanent deletion
- The 🗑️ button in both UIs will delete cloud files; set `--read-only` in `EXTRA_FLAGS` to prevent writes

## Mountpoint protection

The template uses `chattr +i` to make mountpoints immutable when unmounted. This prevents accidental writes to the local disk if the mount dies — otherwise `/root/gdrive1` would silently accept files onto the local filesystem when the cloud isn't actually mounted.

The FUSE mount can still mount over an immutable directory; the immutability only blocks direct writes.

## Health check

```bash
/usr/local/bin/check-rclone-mounts [instance-name ...]
```

Checks:
1. systemd unit is active
2. mountpoint is actually mounted
3. directory is listable
4. shellfb API can traverse it
5. log file is recent

Run with no arguments to check all configured mounts.

## Multiple remotes

To add more mounts (second Google Drive, OneDrive, S3, etc.):

1. Create `/etc/rclone-mounts/<name>.env` with a unique `REMOTE` and `MOUNTPOINT`
2. Configure the rclone remote: `rclone config create <remote-name> <type> ...`
3. Start it: `systemctl start rclone-mount@<name>`

Example for a second Google Drive:

```bash
# Get another OAuth token (log in with the other Google account)
rclone authorize "drive"

# Configure
sudo cp /etc/rclone-mounts/gdrive1.env /etc/rclone-mounts/gdrive2.env
sudo nano /etc/rclone-mounts/gdrive2.env  # change REMOTE=gd2:/ MOUNTPOINT=/root/gdrive2
sudo mkdir -p /root/gdrive2 && sudo chattr +i /root/gdrive2

# Set up
sudo bash scripts/gdrive-setup gd2 '<second-token-json>'
```

## Other cloud providers

The same systemd template works for any rclone-supported backend. Replace `google/drive` with the appropriate backend name and credential flow:

| Provider | Backend | Notes |
|---|---|---|
| OneDrive | `onedrive` | OAuth via `rclone config` (interactive) |
| Dropbox | `dropbox` | OAuth via `rclone config` |
| S3 | `s3` | Access key + secret |
| SFTP | `sftp` | SSH key or password |

See `rclone config` and [rclone.org](https://rclone.org/) for backend-specific setup.

## Google Drive client_id warning

If you see:

> This remote uses rclone's shared Google Drive client_id, which is being retired and will stop working during 2026

The mount is using rclone's built-in OAuth client, which Google is phasing out. To avoid disruption:

1. Create your own OAuth client_id at [Google Cloud Console](https://console.cloud.google.com/)
2. Add `client_id` and `client_secret` to the rclone remote config
3. Re-authorize with `rclone authorize "drive" <client_id> <client_secret>`

Your own client_id also gives you a dedicated API quota instead of sharing with all rclone users.

## Troubleshooting

### Mount starts but shows empty

- Check the log: `journalctl -u rclone-mount@<name> -n 50`
- Verify credentials: `rclone lsd <remote-name>:`
- Confirm the remote path exists

### Files appear but are slow to open

- Cold reads pull from the cloud; first access is always slower
- Increase `VFS_CACHE_MAX_SIZE` if you're hitting the cache limit
- Check `du -sh /var/cache/rclone/<name>` to see current cache usage

### Deletes are permanent instead of going to trash

For Google Drive, verify `--drive-use-trash` is not overridden:

```bash
systemctl cat rclone-mount@<name> | grep drive-use-trash
```

The flag defaults to `true`; it should not appear in `EXTRA_FLAGS` unless you explicitly want permanent deletion.

### Mount won't start after reboot

- Ensure it's enabled: `systemctl enable rclone-mount@<name>`
- Check that `/root/.config/rclone/rclone.conf` exists and has mode 600
- Verify the `RCLONE_CONFIG` path in the unit file matches

## Security notes

- `/root/.config/rclone/rclone.conf` contains OAuth tokens in plaintext; ensure it has mode 600
- The preview panel's `/api/delete` and FileBrowser's delete will remove cloud files when acting on mounted paths
- Consider `--read-only` for archival mounts where writes should never happen
- The `check-rclone-mounts` script mints a short-lived shellfb JWT to test API access; if you rotate the `jwt_secret`, the script will fail until `/usr/local/libexec/shellfb-mktoken` is rebuilt

## Performance tuning

For large files or bandwidth-constrained environments:

- Lower `VFS_CACHE_MAX_SIZE` if disk space is tight
- Increase `DIR_CACHE_TIME` to reduce API calls at the cost of stale listings
- Set `POLL_INTERVAL=0` to disable remote change polling entirely
- Add `--tpslimit 5` to `EXTRA_FLAGS` to rate-limit API requests

For low-latency local caching:

- Use `VFS_CACHE_MODE=full` with a generous `VFS_CACHE_MAX_SIZE`
- Put the cache on fast storage: edit `CACHE_DIR` in the `.env` file

## Uninstalling a mount

```bash
sudo systemctl stop rclone-mount@<name>
sudo systemctl disable rclone-mount@<name>
sudo chattr -i /root/<mountpoint>
sudo rmdir /root/<mountpoint>
sudo rm /etc/rclone-mounts/<name>.env
rclone config delete <remote-name>
```
