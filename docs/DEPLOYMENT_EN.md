# 🚀 CourseHub Deployment Guide

> 🌐 简体中文版: [docs/DEPLOYMENT.md](DEPLOYMENT.md) · This is the English edition.

This is the complete deployment manual for **docker-liook (CourseHub)**: from preparing a server to accessing the site via IP/domain, configuring HTTPS reverse proxy, audio/video calls with TURN relay, data backup, and troubleshooting — step-by-step deployment instructions.

> Supported version: `main` branch (single-container single-instance architecture, SQLite single-writer).
> Security checklist and threat model see [SECURITY.md](../SECURITY.md), feature summary see [README](../README.md).

## Table of Contents

- [1. Deployment Preparation](#1-deployment-preparation)
- [2. How to Set IP and Access Address](#2-how-to-set-ip-and-access-address)
- [3. Way 1: Docker Compose Build (Recommended)](#3-way-1-docker-compose-build-recommended)
- [4. Way 2: Pull CI Built Images](#4-way-2-pull-ci-built-images-no-need-to-install-gonode-on-server)
- [5. Way 3: Binary + systemd (No Docker)](#5-way-3-binary--systemd-no-docker)
- [6. Reverse Proxy and HTTPS](#6-reverse-proxy-and-https)
- [7. Firewall / Cloud Security Group](#7-firewall--cloud-security-group)
- [8. Audio/Video Calls and TURN Relay](#8-audio-video-calls-and-turn-relay)
- [9. Object Storage (Optional): S3 / MinIO / WebDAV](#9-object-storage-optional-s3--minio--webdav)
- [10. Regular Maintenance](#10-regular-maintenance)
- [11. FAQ](#11-faq)

---

## 1. Deployment Preparation

### 1.1 Server Requirements

| Item | Minimum Recommendation | Description |
|---|---|---|
| CPU | 2 cores | HLS transcoding consumes CPU; more cores = faster transcoding |
| Memory | 2 GB | Image resident memory is a few dozen MB; surplus during transcoding is sufficient; for small machines, a 1G swap is recommended |
| Disk | Video size × 2 or more | Source files and HLS chunks are both stored in the `data/` directory |
| OS | Any Linux that can run Docker (amd64 / arm64 both work) | Ubuntu 22.04 / Debian 12 / CentOS Stream etc. are all fine |

### 1.2 Software to Install

- **Way 1 / Way 2**: Only Docker is needed (recommended 20.10+, including `docker compose` plugin):

  ```bash
  curl -fsSL https://get.docker.com | sh   # One-click install Docker (includes compose plugin)
  docker compose version                    # Verify
  ```

- **Way 3 (bare binary)**: No Docker needed, but the server needs Go 1.27+ (or cross-compile on another machine and copy over).

### 1.3 A Domain Name (Optional But Strongly Recommended)

For public deployment, **it is strongly recommended** to prepare a domain and resolve it to the server's public IP. Reasons:

1. Browsers only allow using camera/microphone under `https://` or `localhost` — **without HTTPS, audio/video calls cannot be used over the public network**;
2. HTTPS also avoids a number of issues caused by carrier interception or carrier caching such as the "old frontend after upgrade" problem.

It is also possible to run on IP (see Section 2), but there will be no call capabilities and you must accept plaintext `http://`.

---

## 2. How to Set IP and Access Address

This is the part most likely to confuse during deployment; first clarify "which machine, which IP, which port":

```text
Learner Browser
   │  http://192.168.1.10:8080        ← Local network direct access (LAN usage)
   │  http://<server-public-IP>:8080        ← Public network direct access (temporary testing only)
   │  https://course.example.com       ← Domain + HTTPS reverse proxy (recommended for public use)
   ▼
Firewall / Cloud Security Group (allow 80/443; optionally allow 8080)
   ▼
Nginx / Caddy reverse proxy (80/443, optional)
   ▼
CourseHub Container (listens to 8080 inside the container, host mapping ${HOST_PORT:-8080})
   ▼
./data/   ← SQLite database + video/documents/chat images, all in this directory
```

### 2.1 Choose Address by Usage Scenario

| Scenario | Access Address | Config Needed |
|---|---|---|
| Test on the server itself | `http://localhost:8080` | None |
| Give to colleagues on LAN | `http://<server-local-IP>:8080`, e.g. `http://192.168.1.10:8080` | Allow 8080/TCP; `COOKIE_SECURE` keep `false` |
| Public temporary test | `http://<public-IP>:8080` | Allow 8080/TCP in cloud security groups; only suggested for temporary testing |
| Public formal use | `https://your-domain` | Domain A record points to public IP + HTTPS reverse proxy (see Section 6) + `COOKIE_SECURE=true` |

> Check server local IP: `ip addr` or `hostname -I`; public IP is per cloud console (public IP of cloud VMs is mostly NAT mapping, the network interface can only see the local address).

### 2.2 Quick Reference for Port/IP Related Config

| Config Item | Where to Set | What to Fill |
|---|---|---|
| Host port | `.env` `HOST_PORT` | Default `8080`; if you want to use 80, change to `80` (requires root permission, and avoid conflict with reverse proxy) |
| Container port | Generally no change | Fixed `8080` (compose fixed `PORT=8080`) |
| Access address | Browser | `http://<server-IP>:8080` or `https://domain` |
| `COOKIE_SECURE` | `.env` | Set `true` when site-wide HTTPS; when accessing via IP + http **must keep `false`**, otherwise login will fail |
| S3 / WebDAV address | `.env` or backend "Site Settings → File Storage" | Internal/public address of object storage, e.g. `http://<minio-local-IP>:9000` |
| `TURN_URL` | `.env` | e.g. `turn:<server-public-IP>:3478`, multiple items separated by English commas |

> ⚠️ **After changing port**: Access address is `http://server-IP:HOST_PORT`, container inside is still `8080`, no need to change `PORT`.
>
> ⚠️ **After changing IP or domain**: Video calls / WebSocket use the current page source, no need to change address in the application; but if you switch domain remember to align `COOKIE_SECURE` with the reverse proxy certificate.

### 2.3 Common Symptoms of "Incorrect IP Configuration"

| Symptom | Likely Cause |
|---|---|
| Can start on local machine but others cannot connect | Cloud security groups / `ufw` / `firewalld` not allowing 8080, or service only bound to `127.0.0.1` (this project defaults to listening on `0.0.0.0`, no such issue) |
| Can open page but login immediately drops | Using HTTPS domain but `COOKIE_SECURE=false` → set `true`; or using http but set `COOKIE_SECURE=true` |
| Video call "Connecting…" keeps spinning | No https (browser cannot get camera) or no TURN configured (both on symmetric NAT) |
| Cannot use calls when accessing via IP on LAN | Normal: browsers only allow `https` or `localhost` to use camera/microphone |

---

## 3. Way 1: Docker Compose Build (Recommended)

The simplest way: build and start directly on the server:

```bash
# 1. Install Docker (can be skipped if already installed)
curl -fsSL https://get.docker.com | sh

# 2. Pull code
git clone https://github.com/su469843/docker-liook.git
cd docker-liook

# 3. Configure environment variables (at least change admin password!)
cp .env.example .env
vi .env
```

In `.env` **at minimum** change these two items:

```ini
# Admin initial password, change the default admin123
ADMIN_PASSWORD=your-strong-password

# Session signing key: fill in >32 random string (can also be left empty for auto-generation)
# Generate random string: openssl rand -hex 32
JWT_SECRET=
```

Common optional values (all have defaults, change as needed):

```ini
# Host port: if you want users to directly access 80, change to 80 (later access http://server-IP)
HOST_PORT=8080

# Site name (can also be changed in backend later)
SITE_NAME=My Course Site

# Registration mode: open open registration / invite registration via invite code / closed close registration
REGISTER_MODE=invite

# Enable after site-wide HTTPS (Cookie only transmitted over HTTPS)
COOKIE_SECURE=false
```

Then start:

```bash
# 4. Build and start (first build pulls dependencies, takes a few minutes)
docker compose up -d --build

# 5. Verify
docker compose ps        # STATUS shows (healthy) means normal
curl http://127.0.0.1:8080/healthz   # → {"status":"ok","version":"1.0.0+sha.6f179c0",...}; version = current build
docker compose logs -f   # View logs, Ctrl+C to exit
```

### 3.1 First Login

Open **`http://server-IP:8080`** in browser (port depends on the `HOST_PORT` you change):

1. Username `admin`, password = the `ADMIN_PASSWORD` you set in `.env`;
2. Immediately go to **"Admin → User Management"** to create teacher accounts (teacher accounts can only be created by administrator);
3. Go to **"Admin → Registration Settings"** to confirm registration mode (settings here take precedence over `.env` after initial setup).

> All data is on host `./data/` directory (SQLite + video + documents + chat images + `.jwt_secret`), **backup = copy this directory**.

### 3.2 Data Directory Permissions

When using bind mount (`./data:/data`), the host directory must belong to uid `10001` inside the container, otherwise startup fails with "database read-only / cannot write":

```bash
sudo chown -R 10001:10001 ./data
```

---

## 4. Way 2: Pull CI Built Images (No Need to Install Go/Node on Server)

After pushing to GitHub, `.github/workflows/docker.yml` will automatically build **linux/amd64 + arm64** dual-architecture images and push to GHCR. The corresponding image for this repository is:

```
ghcr.io/su469843/docker-liook:latest
ghcr.io/su469843/docker-liook:1.0.0-sha-6f179c0   # version tag generated on every build
```

> **Where the version comes from**: the base version is `version` in `web/package.json` (bump it when releasing), and the build appends the git commit via `scripts/version.sh`. CI pushes a `<version>-sha-<commit>` tag on every build while keeping `latest`. Pin production to a concrete tag — upgrade and rollback are just a different tag in `IMAGE`. The build version is shown in the Actions summary, at `/healthz`, in the login-page footer and in the user menu.

```bash
git clone https://github.com/su469843/docker-liook.git
cd docker-liook

cp .env.example .env && vi .env      # Same as above, at least change ADMIN_PASSWORD

# Pull image and start (IMAGE variable determines which image compose uses; pinned to a version here)
IMAGE=ghcr.io/su469843/docker-liook:1.0.0-sha-6f179c0 docker compose pull
IMAGE=ghcr.io/su469843/docker-liook:1.0.0-sha-6f179c0 docker compose up -d
```

- If image is private, log in first (password uses GitHub PAT, check `read:packages`):
  `echo <PAT> | docker login ghcr.io -u <GitHub-username> --password-stdin`
- Push to other repositories (similar for Aliyun ACR / Docker Hub / private Harbor):

  ```bash
  docker build -t registry.cn-hangzhou.aliyuncs.com/<namespace>/course-hub:latest .
  docker login registry.cn-hangzhou.aliyuncs.com
  docker push registry.cn-hangzhou.aliyuncs.com/<namespace>/course-hub:latest
  # On server: IMAGE=registry.cn-hangzhou.aliyuncs.com/<namespace>/course-hub:latest docker compose up -d
  ```

> If you don't want to install Docker on local machine: push code to GitHub and let GitHub Actions build, server only `docker pull`. The workflow summary prints the version of this build and the matching `docker pull ...:<version>` command.

---

## 5. Way 3: Binary + systemd (No Docker)

Suitable for 512M small machines or environments that already have systemd. The output is a pure static Go binary (frontend embedded, no external runtime dependencies).

```bash
# 1. Build (on machine with Go 1.27+, in project root directory)
./scripts/build-linux.sh amd64        # Produces dist/course-hub-linux-amd64
# Cross-compile on Windows: .\scripts\build-linux.ps1

# 2. Install to server
sudo useradd -r -s /usr/sbin/nologin -d /opt/coursehub coursehub
sudo mkdir -p /opt/coursehub /var/lib/coursehub
sudo cp dist/course-hub-linux-amd64 /opt/coursehub/course-hub
sudo chmod +x /opt/coursehub/course-hub
sudo chown -R coursehub:coursehub /opt/coursehub /var/lib/coursehub

# 3. Configure + systemd
sudo cp deploy/coursehub.env.example /etc/coursehub.env
sudo vi /etc/coursehub.env             # Change ADMIN_PASSWORD etc.
sudo cp deploy/course-hub.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now course-hub

# 4. Verify
systemctl status course-hub
journalctl -u course-hub -f
curl http://127.0.0.1:8080/healthz
```

Note that for systemd version, the data directory is `/var/lib/coursehub` (already allowed via `ReadWritePaths` in unit file), not Docker's `./data`.

---

## 6. Reverse Proxy and HTTPS

For public formal use, it is recommended to **reverse proxy 443 via Nginx or Caddy → forward to local 8080**, so:

- Browser gets HTTPS, camera/microphone (audio/video calls) available;
- With `.env` `COOKIE_SECURE=true`, session cookies are only transmitted over HTTPS;
- On the server side **only trusts `X-Forwarded-For` when the direct side is local/loopback address** (i.e., only trusts its own reverse proxy), so login rate limiting cannot be bypassed by spoofed headers — when using reverse proxy, please be sure to include `X-Forwarded-For` or `X-Real-IP` headers.

### Nginx Example (Includes WebSocket / Large Files / HLS)

```nginx
server {
    listen 80;
    server_name course.example.com;
    return 301 https://$host$request_uri;   # Force redirect to HTTPS
}

server {
    listen 443 ssl http2;
    server_name course.example.com;

    ssl_certificate     /etc/nginx/ssl/fullchain.pem;
    ssl_certificate_key /etc/nginx/ssl/privkey.pem;

    # Must be enabled for uploading large videos; ≥ MAX_UPLOAD_MB in .env
    client_max_body_size 4096m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # WebSocket (chat + call signaling) must pass-through Upgrade
    location /ws {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

### Caddy Example (Automatic HTTPS, Easiest)

```caddy
course.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy natively supports WebSocket and `X-Forwarded-For`, no extra configuration needed.

### HTTPS Deployment Checklist

- [ ] Reverse proxy forwards `/`, `/api`, `/hls`, `/files`, `/ws` to 8080 (config above already covers)
- [ ] Change `.env` to `COOKIE_SECURE=true`, then `docker compose up -d` to rebuild
- [ ] Nginx `client_max_body_size` ≥ upload limit; Caddy default no limit
- [ ] Visit with `https://` to confirm lock icon; `curl https://domain/healthz` returns `{"status":"ok","version":"..."}`

---

## 7. Firewall / Cloud Security Group

Allow ports based on deployment type (taking Ubuntu `ufw` as an example, cloud servers also need to allow in console security groups):

| Deployment Type | Port to Allow | Example Command |
|---|---|---|
| LAN http direct | 8080/tcp | `sudo ufw allow 8080/tcp` |
| Public http temporary test | 8080/tcp | Same as above (not recommended long-term exposure) |
| HTTPS reverse proxy | 80/tcp + 443/tcp | `sudo ufw allow 80,443/tcp` |
| Self-hosted TURN | 3478/tcp+udp, 49160-49200/udp | `sudo ufw allow 3478` `sudo ufw allow 49160:49200/udp` |

> Remember to keep SSH port 22 open. **8080 does not need to be opened to the public network** (when only bound with reverse proxy), more secure.

---

## 8. Audio/Video Calls and TURN Relay

WebRTC calls usually use STUN negotiation; as long as one side is in a normal home/office network, direct connection works. The following cases require self-hosted TURN (e.g., coturn):

- Both parties are behind symmetric NAT / corporate networks, picture stays in "Connecting…";
- Corporate network blocks UDP, needs TURN over TCP 443.

Configure in `.env`:

```ini
TURN_URL=turn:turn.example.com:3478    # Or turn:<public-IP>:3478, multiple separated by commas
TURN_SECRET=replace-with-coturn-static-auth-secret   # Recommended use-auth-secret mode, issues 2-hour temporary account
```

Key coturn config (`/etc/turnserver.conf`):

```
listening-port=3478
fingerprint
use-auth-secret
static-auth-secret=must be same value as TURN_SECRET
realm=course.example.com
min-port=49160
max-port=49200
external-ip=<public-IP>/<local-IP>   # Must configure for cloud VMs behind NAT
```

Also allow `3478/tcp+udp` and `49160-49200/udp` on firewall. More troubleshooting see [README → 通话排障速查](../README.md#通话排障速查).

---

## 9. Object Storage (Optional): S3 / MinIO / WebDAV

By default when `STORAGE_TYPE=local`, video chunks, documents, images are all in `./data`. To save server disk space, you can switch to S3/WebDAV in the backend **"Site Settings → File Storage"** with one click (can first "Test Connection"), or directly use environment variables:

```ini
STORAGE_TYPE=s3
S3_ENDPOINT=<minio-local-IP>:9000        # ⚠️ Only fill host:port, do not include http:// — whether to use HTTPS controlled by S3_USE_SSL
S3_BUCKET=coursehub
S3_ACCESS_KEY=xxx
S3_SECRET_KEY=xxxx
S3_USE_SSL=false
```

- The compose file includes commented-out **MinIO** service; uncomment and set `STORAGE_TYPE=s3` to run together;
- **Do not enable public read for the storage bucket**; chunks are encrypted and addresses are signed; public read would bypass protection;
- After connecting to object storage, only SQLite database remains on local machine, backup is lighter.

---

## 10. Regular Maintenance

### Backup and Restore

```bash
# Backup (recommended first docker compose down or during off-peak, direct copy during SQLite running may be inconsistent)
sudo tar czf coursehub-backup-$(date +%F).tar.gz ./data
# If using S3/WebDAV, also save to object storage bucket

# Restore: put data directory back in place (note to chown 10001:10001), then up -d
```

### Upgrade

```bash
git pull
docker compose up -d --build      # Or docker compose pull && docker compose up -d
```

- Data directory migrates automatically; back up `./data` before upgrading.
- After upgrading, run `curl http://127.0.0.1:8080/healthz` and confirm `version` changed; the login-page footer / user menu also show the running build.
- With CI images, **pin a concrete version** instead of `latest`; rolling back is just changing the tag in `IMAGE` and running `docker compose up -d` (see section 4).

### Forgot Admin Password

`ADMIN_PASSWORD` is **only used to create admin during the first database initialization**; changing environment variables afterward will not change the password:

```bash
# Fresh environment (will lose data, use with caution)
docker compose down && sudo rm -f ./data/coursehub.db* && docker compose up -d

# If there are other admins: reset password in "Admin → User Management"
```

### Health Check and Graceful Shutdown

- `GET /healthz` does not require login, Docker `HEALTHCHECK` is built-in; `docker compose ps` showing `(healthy)` is normal;
- `docker stop` / `systemctl stop` will wait until requests are handled before closing the database, will not lose data currently being written.

### Common Commands

```bash
docker compose ps                     # Status (healthy)
docker compose logs -f --tail 200     # Logs
docker compose restart                 # Restart
docker compose down                    # Stop (data remains in ./data)
docker compose down && sudo rm -f ./data/coursehub.db* && docker compose up -d  # Clear and rebuild database (will lose data!)
```

---

## 11. FAQ

| Question | Solution |
|---|---|
| Cannot open page | Check `docker compose ps` if healthy; use `curl http://127.0.0.1:8080/healthz` to distinguish "service didn't start" from "firewall didn't allow"; allow ports in security groups / `ufw` |
| Login fails but password is correct | Four consecutive failed login attempts trigger rate limiting (same IP+username for 15 minutes); wait a few minutes; or confirm not using `COOKIE_SECURE=true` with http |
| Upload video reports "File exceeds limit" | Direct upload limited by `MAX_UPLOAD_MB` (default 4096MB); for larger files use "chunked upload" in the upload dialog; when using Nginx remember `client_max_body_size` ≥ that value |
| Video stuck in "Transcoding" | Server-side ffmpeg transcoding is work-intensive, slower on small machines is normal; after restart, the currently transcoding class will be marked as failed and needs to be re-uploaded (known limitation) |
| Call fails with "Connecting…" | No https or no TURN, see Sections 6/8 above |
| Want learners to only view, not register | In backend "Registration Settings" choose "Closed Registration", all accounts created by administrator |
| Frontend white screen | First `Ctrl+F5` hard refresh; if still abnormal, try `CSP_DISABLE=true` in `.env` to restart for troubleshooting, remember to turn it off after solving |

---

## Deployment Checklist (One-Line Version)

```text
Change ADMIN_PASSWORD → Set JWT_SECRET → Choose access mode (local IP:8080 / domain HTTPS) →
Configure reverse proxy (WS + large files) → COOKIE_SECURE=true → Allow security group ports → chown 10001 ./data →
(Optional) S3/MinIO, TURN → Backup ./data
```

If there's an issue, first check `docker compose logs -f` and `GET /healthz`; for the rest see [README troubleshooting section](../README.md).
