# Deploying the storefront

The site is a Go binary + Postgres + ~13 GB of image files, so it runs on a **Hostinger VPS
(Docker template)**, not on Web/Cloud hosting. The GitHub "deploy from repository" feature on
Web/Cloud plans only builds Node.js and static sites, which is why the build failed with
`Missing script: "build"`.

Every push to `main` runs `.github/workflows/deploy.yml`: it builds the image, pushes it to
`ghcr.io/h3bzzz/georges-showroom`, then asks Hostinger to `docker compose up` the stack in
`docker-compose.yml` (Postgres 16, the app, Caddy with automatic HTTPS).

## One-time setup

1. Order a KVM VPS (KVM 2 or larger: the data set alone needs ~15 GB and Postgres wants RAM)
   and pick the **Docker** OS template. Point the site's DNS A record at the VPS IP.
2. hPanel → API → create a key. The VM ID is the number in the server hostname
   (`srv123456.hstgr.cloud` → `123456`).
3. GitHub repo → Settings → Secrets and variables → Actions:
   - secret `HOSTINGER_API_KEY`
   - secret `POSTGRES_PASSWORD` (anything long)
   - variable `HOSTINGER_VM_ID`
   - variable `DOMAIN` (e.g. `shop.georgesshowroom.com`)
4. After the first workflow run, open the package at github.com/h3bzzz?tab=packages and set
   `georges-showroom` to **public** so the VPS can pull it without a registry login.
5. Copy the data the storefront serves (everything but the embedded Postgres and the raw
   catalogs/webimg masters is optional, but simplest is the whole folder):

       rsync -a --info=progress2 --exclude pg --exclude tools --exclude cache \
         data/raw/ root@VPS_IP:/srv/georges/data/

6. Load the database. The embedded Postgres ships no `pg_dump`, so install the client once on
   the workstation (`sudo pacman -S postgresql-libs` on Arch), dump while `georges serve` or
   `georges db start` is running, and restore into the container on the VPS:

       pg_dump -Fc "postgres://georges:georges@localhost:5432/georges" -f georges.dump
       scp georges.dump root@VPS_IP:/root/
       ssh root@VPS_IP 'DB=$(docker ps -qf name=db); docker cp /root/georges.dump $DB:/tmp/ && \
         docker exec $DB pg_restore -U georges -d georges --no-owner --clean --if-exists /tmp/georges.dump'

Repeat steps 5 and 6 whenever the pipeline on the workstation produces new crops or rows.
