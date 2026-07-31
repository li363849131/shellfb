# Design : ShellFromBrowser — Traversée réseau et déploiement simplifié

**Date :** 2026-05-15
**Statut :** Validé (en attente d'implémentation)
**Approche retenue :** C — Hybride progressive

## Contexte et problème

ShellFromBrowser est un outil qui fournit un terminal distant accessible depuis
un navigateur. Dans les environnements réseau contraints (aéroports, gares,
centres commerciaux, entreprises), seuls les ports 53, 80 et 443 sont
autorisés en sortie. L'outil doit fonctionner dans ces conditions sans
configuration spéciale côté client.

ShellFromBrowser est déjà "traversant" par nature : le navigateur fait du
HTTPS/WebSocket standard sur le port 443. Aucun firewall ne peut distinguer
cette connexion d'une visite sur un site web ordinaire.

Ce qui manque : une expérience de déploiement simplifiée pour que n'importe qui
puisse mettre l'outil en production sur le port 443 avec TLS, sans
connaissance réseau préalable.

## Public cible

Le plus large possible — de l'administrateur système expérimenté au
développeur qui veut juste accéder à son serveur depuis n'importe où.

## Décisions de conception

### Configuration 12-factor

Chaque paramètre est configurable via 4 sources, par ordre de priorité
décroissante :

```text
Variable d'environnement  >  Flag CLI  >  config.yaml  >  Valeur par défaut
```

#### Variables d'environnement

| Env var | Flag CLI | Config YAML | Défaut |
|---------|----------|-------------|--------|
| `SHELLFB_ADDR` | `--addr` | `server.addr` | `:8080` |
| `SHELLFB_DOMAIN` | `--domain` | `server.domain` | _(vide)_ |
| `SHELLFB_TLS_CERT` | `--tls-cert` | `server.tls.cert` | _(vide)_ |
| `SHELLFB_TLS_KEY` | `--tls-key` | `server.tls.key` | _(vide)_ |
| `SHELLFB_AUTOCERT_DIR` | `--autocert-dir` | `server.autocert_dir` | `~/.shellfb/certs` |
| `SHELLFB_AUTH_ENABLED` | — | `auth.enabled` | `false` |
| `SHELLFB_JWT_SECRET` | — | `auth.jwt_secret` | _(vide)_ |

#### Nouveau config.yaml

```yaml
server:
  addr: ":443"
  domain: "shell.example.com"    # active auto-TLS si renseigné
  autocert_dir: ""               # défaut : ~/.shellfb/certs
  tls:
    enabled: false               # ignoré si domain est renseigné
    cert: ""
    key: ""

auth:
  enabled: false
  jwt_secret: "change-me-to-a-random-string"
  users:
    - username: admin
      password_hash: ""

shell:
  command: ""
  env:
    - "TERM=xterm-256color"

sessions:
  max_per_user: 10
  idle_timeout: "30m"

ssh:
  enabled: true
  known_hosts: "~/.ssh/known_hosts"

recording:
  enabled: false
  dir: "./recordings"
```

#### Logique de résolution TLS

```text
Si domain est défini :
  → Auto-TLS (Let's Encrypt)
  → Écoute sur :443 (HTTPS) et :80 (challenge + redirection)
  → Erreur si tls.cert ou tls.key sont aussi renseignés (conflit)
Sinon si tls.cert ET tls.key sont définis :
  → TLS manuel sur le port configuré dans addr
Sinon :
  → HTTP simple sur le port configuré dans addr
  → Adapté au dev local ou derrière un reverse proxy
```

### Auto-TLS intégré

#### Dépendance

```text
golang.org/x/crypto/acme/autocert
```

Bibliothèque officielle de l'équipe Go. Stable, maintenue, utilisée par
Caddy, Traefik et des milliers de projets en production.

#### Fonctionnement

1. Au démarrage, si `domain` est configuré, création d'un `autocert.Manager`
2. Écoute sur :443 avec le `tls.Config` fourni par autocert
3. Écoute sur :80 pour :
   - Le challenge HTTP-01 de Let's Encrypt (validation du domaine)
   - La redirection HTTP → HTTPS (301) pour tout autre trafic
4. Les certificats sont stockés dans `autocert_dir`
5. Le renouvellement est automatique (avant expiration)

#### Stockage des certificats

Répertoire par défaut : `~/.shellfb/certs` (standalone) ou
`/var/lib/shellfb/certs` (Docker).

Le dossier doit être persisté (volume Docker) pour éviter de re-négocier
avec Let's Encrypt à chaque redémarrage.

#### Contraintes

- Les ports 80 ET 443 doivent être accessibles depuis Internet
- Le DNS du domaine doit pointer vers l'IP du serveur
- Let's Encrypt a une limite de 50 certificats/semaine/domaine (largement
  suffisant pour un usage normal)

#### Messages au démarrage

```text
[INFO] Auto-TLS: obtaining certificate for shell.example.com...
[INFO] Listening on :443 (HTTPS) and :80 (HTTP redirect)
[INFO] Certificate obtained successfully, expires 2026-08-13
```

En cas d'erreur :

```text
[ERROR] Auto-TLS failed: could not verify domain ownership.
        Ensure port 80 is accessible from the internet and
        DNS for shell.example.com points to this server.
```

### Docker

#### One-liner

```bash
docker run -d --name shellfb \
  -p 80:80 -p 443:443 \
  -e SHELLFB_DOMAIN=shell.example.com \
  -v shellfb-certs:/var/lib/shellfb/certs \
  valorisa/shellfb
```

#### docker-compose.yml

```yaml
services:
  shellfb:
    image: valorisa/shellfb
    ports:
      - "80:80"
      - "443:443"
    environment:
      - SHELLFB_DOMAIN=shell.example.com
      - SHELLFB_AUTH_ENABLED=true
      - SHELLFB_JWT_SECRET=change-me-to-a-random-string
    volumes:
      - certs:/var/lib/shellfb/certs
      - ./config.yaml:/etc/shellfb/config.yaml:ro
    restart: unless-stopped

volumes:
  certs:
```

#### Variante reverse proxy

```yaml
services:
  shellfb:
    image: valorisa/shellfb
    environment:
      - SHELLFB_ADDR=:8080
    expose:
      - "8080"

  caddy:
    image: caddy:2
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile
      - caddy-data:/data

volumes:
  caddy-data:
```

Caddyfile correspondant :

```text
shell.example.com {
    reverse_proxy shellfb:8080
}
```

#### Dockerfile

Modifications par rapport à l'actuel :

- `EXPOSE 80 443` (au lieu de 8080 seul)
- Entrypoint : `shellfb --config /etc/shellfb/config.yaml`
- Utilisateur non-root avec `NET_BIND_SERVICE` pour ports < 1024
- Répertoire `/var/lib/shellfb/certs` créé avec les bonnes permissions

### Documentation réseau contraint

#### Fichier dédié

`docs/deploiement-reseaux-contraints.md` contenant :

1. Explication du problème (ports bloqués, DPI)
2. Schéma montrant pourquoi ShellFromBrowser passe (HTTPS standard)
3. Guide pas à pas — 3 scénarios :
   - Auto-TLS (le plus simple)
   - Derrière un reverse proxy
   - Docker one-liner
4. Tableau des ports requis (côté serveur vs côté client)
5. FAQ (port 80 pris, sans nom de domaine, proxy d'entreprise)

#### Résumé dans le README

Section courte (~10 lignes) dans le README principal avec lien vers le guide
complet.

## Modes de fonctionnement (récapitulatif)

| Mode | Config | Ports | Cas d'usage |
|------|--------|-------|-------------|
| Auto-TLS | `domain: "x.com"` | 80 + 443 | Production, le plus simple |
| TLS manuel | `tls.cert` + `tls.key` | Celui dans `addr` | Certificat wildcard, custom CA |
| HTTP simple | Rien | Celui dans `addr` (défaut 8080) | Dev local, derrière reverse proxy |

## Fichiers impactés

| Fichier | Modification |
|---------|-------------|
| `internal/config/config.go` | Ajout champ `Domain`, `AutocertDir`, lecture env vars |
| `internal/server/server.go` | Listener auto-TLS, double-port, redirection HTTP |
| `cmd/shellfb/main.go` | Flags `--domain`, `--tls-cert`, `--tls-key`, `--autocert-dir` |
| `config.example.yaml` | Ajout `domain`, `autocert_dir` |
| `Dockerfile` | Expose 80+443, user non-root, volume certs |
| `docker-compose.yml` | Mise à jour avec env vars et volume |
| `docs/deploiement-reseaux-contraints.md` | Nouveau fichier |
| `README.md` / `README.fr.md` | Section réseau contraint |
| `go.mod` | Ajout `golang.org/x/crypto` (si pas déjà présent) |

## Future work (phase 2 — non implémenté)

| Fonctionnalité | Cas d'usage | Complexité |
|----------------|-------------|------------|
| Proxy HTTP CONNECT pour SSH sortant | ShellFromBrowser dans un réseau corporate bloquant le port 22 sortant | Moyenne |
| Support plateformes cloud (Fly.io, Railway) | Déploiement sans serveur dédié | Faible (config + docs) |
| Health check endpoint (`/healthz`) | Monitoring, load balancer | Faible |
| Redirection HTTP→HTTPS sans auto-TLS | Reverse proxy exposant les deux ports | Faible |

## Critères de succès

- [ ] `shellfb --domain shell.example.com` démarre et obtient un certificat
- [ ] `docker run -e SHELLFB_DOMAIN=x.com` fonctionne en one-liner
- [ ] L'outil est accessible depuis un réseau qui n'autorise que le port 443
- [ ] Les env vars, flags CLI, et config.yaml fonctionnent avec la bonne priorité
- [ ] La documentation permet à un non-expert de déployer en moins de 10 minutes
- [ ] Aucune régression sur le mode HTTP simple existant
