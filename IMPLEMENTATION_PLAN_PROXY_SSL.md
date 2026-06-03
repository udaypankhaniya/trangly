# Implementation Plan: Proxy Manager & SSL Certificates

## Overview

Add Nginx reverse proxy setup + SSL certificate generation to Trangly. No DNS management.

**Scope:**
- SSL certificate generation (Let's Encrypt ACME or self-signed)
- Nginx reverse proxy configuration per domain
- Domain → backend port mapping
- Web UI for domain/proxy management

**Complexity:** Medium | **Story Points:** 20 | **Execution Waves:** 3

---

## Architecture Decisions

### 1. Domain Model

```
Project → [Domain] → [ProxyConfig]
                  → [SSLCertificate]
```

One project can serve multiple domains. Each domain maps to container port.

### 2. Proxy Strategy

**Inline Nginx config in SQLite:**
- Trangly generates Nginx config snippets per domain
- Config mounted into deployment container via `docker-compose.yml`
- No separate Nginx container — single-binary simplicity
- User manually configures DNS A record → Trangly server IP

### 3. SSL Certificate Provider

**ACME (Let's Encrypt) with HTTP-01 challenge:**
- Simple HTTP validation (requires `:80` access)
- Certificate stored encrypted in SQLite
- 90-day renewal check at startup + scheduler
- Self-signed fallback for dev/testing

**Why HTTP-01 (not DNS-01):**
- No DNS provider integration needed
- Simpler ACME flow
- Standard `lego` library support
- Easier testing (mock HTTP server)

---

## Database Schema Changes

### Migration 005: Add domain, SSL & proxy tables

```sql
CREATE TABLE IF NOT EXISTS domains (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    domain_name     TEXT NOT NULL,           -- e.g. "example.com"
    subdomain       TEXT,                    -- e.g. "api"; NULL for apex
    container_port  INTEGER NOT NULL,        -- internal service port
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending, validating, active, failed, expired
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, domain_name, subdomain)
);

CREATE INDEX idx_domains_project_id ON domains(project_id);
CREATE INDEX idx_domains_status ON domains(status);

CREATE TABLE IF NOT EXISTS ssl_certificates (
    id              TEXT PRIMARY KEY,
    domain_id       TEXT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    cert_pem        BLOB NOT NULL,           -- AES-256-GCM encrypted
    key_pem         BLOB NOT NULL,           -- AES-256-GCM encrypted
    chain_pem       BLOB,                    -- AES-256-GCM encrypted
    issued_at       DATETIME,
    expires_at      DATETIME NOT NULL,
    renewal_status  TEXT DEFAULT 'none',
    renewal_error   TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_ssl_domain_id ON ssl_certificates(domain_id);
CREATE INDEX idx_ssl_expires ON ssl_certificates(expires_at);

CREATE TABLE IF NOT EXISTS proxy_configs (
    id              TEXT PRIMARY KEY,
    domain_id       TEXT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    config_nginx    TEXT NOT NULL,           -- Full Nginx server block
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_proxy_domain_id ON proxy_configs(domain_id);
```

---

## Layer-by-Layer Implementation

### Layer 1: Domain Models (`internal/domain/`)

**File: `domain_binding.go`**
```go
package domain

import "time"

type Domain struct {
    ID            string
    ProjectID     string
    DomainName    string    // "example.com"
    Subdomain     *string   // "api" or nil for apex
    ContainerPort int       // 8080
    Status        string    // pending, validating, active, failed, expired
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

func (d *Domain) FullDomain() string {
    if d.Subdomain != nil && *d.Subdomain != "" {
        return *d.Subdomain + "." + d.DomainName
    }
    return d.DomainName
}

type SSLCertificate struct {
    ID            string
    DomainID      string
    CertPEM       []byte    // (encrypted)
    KeyPEM        []byte    // (encrypted)
    ChainPEM      []byte    // (encrypted)
    IssuedAt      *time.Time
    ExpiresAt     time.Time
    RenewalStatus string
    RenewalError  *string
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

func (c *SSLCertificate) IsExpiringSoon() bool {
    return time.Until(c.ExpiresAt) < 30*24*time.Hour
}

type ProxyConfig struct {
    ID          string
    DomainID    string
    ConfigNginx string
    Status      string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

**File: `enums.go`** (append)
```go
const (
    DomainStatusPending    = "pending"
    DomainStatusValidating = "validating"
    DomainStatusActive     = "active"
    DomainStatusFailed     = "failed"
    DomainStatusExpired    = "expired"
)

const (
    ProxyStatusActive   = "active"
    ProxyStatusInactive = "inactive"
)

const (
    CertRenewalNone    = "none"
    CertRenewalPending = "pending"
    CertRenewalSuccess = "success"
    CertRenewalFailed  = "failed"
)
```

---

### Layer 2: Infrastructure (`internal/infra/`)

**File: `infra/domain/domain_repo.go`**
```go
package domainrepo

import (
    "context"
    "database/sql"
    "github.com/udaypankhaniya/trangly/internal/domain"
    "github.com/udaypankhaniya/trangly/internal/infra/db"
)

type DomainRepository struct {
    db *sql.DB
    q  *db.Queries
}

func NewDomainRepository(db *sql.DB, q *db.Queries) *DomainRepository {
    return &DomainRepository{db: db, q: q}
}

func (r *DomainRepository) CreateDomain(ctx context.Context, d *domain.Domain) error {
    return r.q.InsertDomain(ctx, db.InsertDomainParams{
        ID:            d.ID,
        ProjectID:     d.ProjectID,
        DomainName:    d.DomainName,
        Subdomain:     d.Subdomain,
        ContainerPort: int64(d.ContainerPort),
        Status:        d.Status,
    })
}

func (r *DomainRepository) GetDomainsByProjectID(ctx context.Context, projectID string) ([]*domain.Domain, error) {
    rows, err := r.q.GetDomainsByProjectID(ctx, projectID)
    if err != nil {
        return nil, err
    }
    var domains []*domain.Domain
    for _, row := range rows {
        domains = append(domains, &domain.Domain{
            ID:            row.ID,
            ProjectID:     row.ProjectID,
            DomainName:    row.DomainName,
            Subdomain:     row.Subdomain,
            ContainerPort: int(row.ContainerPort),
            Status:        row.Status,
            CreatedAt:     row.CreatedAt,
            UpdatedAt:     row.UpdatedAt,
        })
    }
    return domains, nil
}

func (r *DomainRepository) UpdateDomainStatus(ctx context.Context, domainID, status string) error {
    return r.q.UpdateDomainStatus(ctx, db.UpdateDomainStatusParams{
        ID:     domainID,
        Status: status,
    })
}

func (r *DomainRepository) DeleteDomain(ctx context.Context, domainID string) error {
    return r.q.DeleteDomain(ctx, domainID)
}
```

**File: `infra/ssl/acme_client.go`**
```go
package ssl

import (
    "context"
    "crypto/rand"
    "crypto/rsa"
    "crypto/x509"
    "encoding/pem"
    "time"
    
    "github.com/go-acme/lego/v4/certificate"
    "github.com/go-acme/lego/v4/challenge/http01"
    "github.com/go-acme/lego/v4/lego"
    "github.com/go-acme/lego/v4/registration"
    "github.com/udaypankhaniya/trangly/internal/domain"
)

type ACMEClient struct {
    client *lego.Client
}

func NewACMEClient(email string, production bool) (*ACMEClient, error) {
    config := lego.NewConfig(&User{Email: email})
    if production {
        config.CADirURL = lego.LEDirectoryProduction
    } else {
        config.CADirURL = lego.LEDirectoryStaging
    }
    
    client, err := lego.NewClient(config)
    if err != nil {
        return nil, err
    }
    
    reg, err := client.Registration.Register(context.Background(), registration.RegisterOptions{
        TermsOfServiceAgreed: true,
    })
    if err != nil {
        return nil, err
    }
    client.Registration = reg
    
    // HTTP-01 challenge on :80
    err = client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "80"))
    if err != nil {
        return nil, err
    }
    
    return &ACMEClient{client: client}, nil
}

func (ac *ACMEClient) IssueCertificate(ctx context.Context, fullDomain string) (*domain.SSLCertificate, error) {
    request := certificate.ObtainRequest{Domains: []string{fullDomain}}
    cert, err := ac.client.Certificate.Obtain(request)
    if err != nil {
        return nil, err
    }
    
    x509Cert, _ := parseCert(cert.Certificate)
    return &domain.SSLCertificate{
        CertPEM:   cert.Certificate,
        KeyPEM:    cert.PrivateKey,
        ChainPEM:  cert.IssuerCertificate,
        IssuedAt:  &x509Cert.NotBefore,
        ExpiresAt: x509Cert.NotAfter,
    }, nil
}

func (ac *ACMEClient) RenewCertificate(ctx context.Context, cert *domain.SSLCertificate) (*domain.SSLCertificate, error) {
    resource := certificate.Resource{
        Certificate:       cert.CertPEM,
        PrivateKey:        cert.KeyPEM,
        IssuerCertificate: cert.ChainPEM,
    }
    
    newCert, err := ac.client.Certificate.Renew(resource, true, false)
    if err != nil {
        return nil, err
    }
    
    x509Cert, _ := parseCert(newCert.Certificate)
    return &domain.SSLCertificate{
        CertPEM:   newCert.Certificate,
        KeyPEM:    newCert.PrivateKey,
        ChainPEM:  newCert.IssuerCertificate,
        IssuedAt:  &x509Cert.NotBefore,
        ExpiresAt: x509Cert.NotAfter,
    }, nil
}

func GenerateSelfSigned(fullDomain string) (*domain.SSLCertificate, error) {
    privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
    
    now := time.Now()
    cert := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: fullDomain},
        NotBefore:    now,
        NotAfter:     now.AddDate(1, 0, 0),
        KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
        ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        DNSNames:     []string{fullDomain},
    }
    
    certBytes, _ := x509.CreateCertificate(rand.Reader, cert, cert, &privateKey.PublicKey, privateKey)
    certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
    keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
    
    return &domain.SSLCertificate{
        CertPEM:   certPEM,
        KeyPEM:    keyPEM,
        IssuedAt:  &now,
        ExpiresAt: now.AddDate(1, 0, 0),
    }, nil
}
```

---

### Layer 3: App Services (`internal/app/`)

**File: `app/domain_service.go`**
```go
package app

import (
    "context"
    "errors"
    "fmt"
    "log/slog"
    
    "github.com/udaypankhaniya/trangly/internal/domain"
    "github.com/udaypankhaniya/trangly/internal/infra/crypto"
    "github.com/udaypankhaniya/trangly/internal/infra/ssl"
    "github.com/udaypankhaniya/trangly/pkg/idgen"
)

type DomainService struct {
    domainRepo *DomainRepository
    sslRepo    *SSLRepository
    proxyRepo  *ProxyRepository
    acmeClient *ssl.ACMEClient
    cipher     *crypto.Cipher
    logger     *slog.Logger
}

type CreateDomainRequest struct {
    ProjectID     string
    DomainName    string
    Subdomain     *string
    ContainerPort int
}

func (s *DomainService) CreateDomain(ctx context.Context, req CreateDomainRequest) (*domain.Domain, error) {
    // Validate project exists
    if _, err := s.projectRepo.GetByID(ctx, req.ProjectID); err != nil {
        return nil, fmt.Errorf("project not found: %w", err)
    }
    
    // Validate domain format
    if !isValidDomain(req.DomainName) {
        return nil, errors.New("invalid domain format")
    }
    
    // Check unique constraint
    existing, _ := s.domainRepo.GetByProjectAndDomain(ctx, req.ProjectID, req.DomainName, req.Subdomain)
    if existing != nil {
        return nil, errors.New("domain already attached to this project")
    }
    
    d := &domain.Domain{
        ID:            idgen.New(),
        ProjectID:     req.ProjectID,
        DomainName:    req.DomainName,
        Subdomain:     req.Subdomain,
        ContainerPort: req.ContainerPort,
        Status:        domain.DomainStatusValidating,
    }
    
    if err := s.domainRepo.CreateDomain(ctx, d); err != nil {
        return nil, err
    }
    
    // Trigger ACME certificate request (async)
    go s.requestSSLCertificate(context.Background(), d)
    
    return d, nil
}

func (s *DomainService) requestSSLCertificate(ctx context.Context, d *domain.Domain) {
    fullDomain := d.FullDomain()
    
    var cert *domain.SSLCertificate
    var err error
    
    // Try ACME; fall back to self-signed on error
    cert, err = s.acmeClient.IssueCertificate(ctx, fullDomain)
    if err != nil {
        s.logger.Warn("ACME failed; using self-signed", "domain", fullDomain, "err", err)
        cert, err = ssl.GenerateSelfSigned(fullDomain)
        if err != nil {
            s.logger.Error("failed to generate certificate", "domain", fullDomain, "err", err)
            s.domainRepo.UpdateDomainStatus(ctx, d.ID, domain.DomainStatusFailed)
            return
        }
    }
    
    // Encrypt certificate & key
    certEnc, _ := s.cipher.Encrypt(cert.CertPEM)
    keyEnc, _ := s.cipher.Encrypt(cert.KeyPEM)
    var chainEnc []byte
    if cert.ChainPEM != nil {
        chainEnc, _ = s.cipher.Encrypt(cert.ChainPEM)
    }
    
    cert.ID = idgen.New()
    cert.DomainID = d.ID
    cert.CertPEM = certEnc
    cert.KeyPEM = keyEnc
    cert.ChainPEM = chainEnc
    
    // Store certificate
    if err := s.sslRepo.CreateSSLCertificate(ctx, cert); err != nil {
        s.logger.Error("failed to store certificate", "err", err)
        return
    }
    
    // Generate proxy config
    if err := s.generateProxyConfig(ctx, d); err != nil {
        s.logger.Error("failed to generate proxy config", "err", err)
        return
    }
    
    // Mark domain active
    s.domainRepo.UpdateDomainStatus(ctx, d.ID, domain.DomainStatusActive)
    s.logger.Info("domain certificate issued", "domain", fullDomain)
}

func (s *DomainService) generateProxyConfig(ctx context.Context, d *domain.Domain) error {
    config := fmt.Sprintf(`
upstream backend_%s {
    server 127.0.0.1:%d;
}

server {
    listen 443 ssl http2;
    server_name %s;
    
    ssl_certificate     /etc/ssl/certs/%s.crt;
    ssl_certificate_key /etc/ssl/private/%s.key;
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_ciphers         HIGH:!aNULL:!MD5;
    
    location / {
        proxy_pass http://backend_%s;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

server {
    listen 80;
    server_name %s;
    location /.well-known/acme-challenge/ { root /var/www/letsencrypt; }
    location / { return 301 https://$server_name$request_uri; }
}
`, d.ID, d.ContainerPort, d.FullDomain(), d.ID, d.ID, d.ID, d.FullDomain())
    
    pc := &domain.ProxyConfig{
        ID:          idgen.New(),
        DomainID:    d.ID,
        ConfigNginx: config,
        Status:      domain.ProxyStatusActive,
    }
    
    return s.proxyRepo.CreateProxyConfig(ctx, pc)
}

func (s *DomainService) RenewExpiring(ctx context.Context) error {
    certs, err := s.sslRepo.GetExpiringCerts(ctx, 30*24*time.Hour)
    if err != nil {
        return err
    }
    
    for _, cert := range certs {
        d, err := s.domainRepo.GetByID(ctx, cert.DomainID)
        if err != nil {
            s.logger.Error("domain not found", "cert_id", cert.ID, "err", err)
            continue
        }
        s.requestSSLCertificate(ctx, d)
    }
    
    return nil
}

func (s *DomainService) DeleteDomain(ctx context.Context, domainID string) error {
    return s.domainRepo.DeleteDomain(ctx, domainID)
}

func (s *DomainService) ListDomainsByProject(ctx context.Context, projectID string) ([]*domain.Domain, error) {
    return s.domainRepo.GetDomainsByProjectID(ctx, projectID)
}
```

---

### Layer 4: API Handlers (`internal/api/http/handlers/`)

**File: `api/http/handlers/domain.go`**
```go
package handlers

import (
    "errors"
    "github.com/gofiber/fiber/v2"
    "github.com/udaypankhaniya/trangly/internal/app"
)

type DomainHandler struct {
    domainSvc *app.DomainService
}

func NewDomainHandler(domainSvc *app.DomainService) *DomainHandler {
    return &DomainHandler{domainSvc: domainSvc}
}

func (h *DomainHandler) Create(c *fiber.Ctx) error {
    projectID := c.Params("id")
    
    var req struct {
        DomainName    string `json:"domain_name"`
        Subdomain     string `json:"subdomain"`
        ContainerPort int    `json:"container_port"`
    }
    
    if err := c.BodyParser(&req); err != nil {
        return respondError(c, fiber.StatusBadRequest, "invalid request")
    }
    
    ctx, cancel := requestCtx(c)
    defer cancel()
    
    var subdomain *string
    if req.Subdomain != "" {
        subdomain = &req.Subdomain
    }
    
    domain, err := h.domainSvc.CreateDomain(ctx, app.CreateDomainRequest{
        ProjectID:     projectID,
        DomainName:    req.DomainName,
        Subdomain:     subdomain,
        ContainerPort: req.ContainerPort,
    })
    
    if err != nil {
        return respondError(c, fiber.StatusBadRequest, err.Error())
    }
    
    return respondJSON(c, fiber.StatusCreated, domain)
}

func (h *DomainHandler) List(c *fiber.Ctx) error {
    projectID := c.Params("id")
    
    ctx, cancel := requestCtx(c)
    defer cancel()
    
    domains, err := h.domainSvc.ListDomainsByProject(ctx, projectID)
    if err != nil {
        return respondError(c, fiber.StatusInternalServerError, err.Error())
    }
    
    return respondJSON(c, fiber.StatusOK, fiber.Map{"domains": domains})
}

func (h *DomainHandler) Delete(c *fiber.Ctx) error {
    domainID := c.Params("domainId")
    
    ctx, cancel := requestCtx(c)
    defer cancel()
    
    if err := h.domainSvc.DeleteDomain(ctx, domainID); err != nil {
        return respondError(c, fiber.StatusNotFound, "domain not found")
    }
    
    return c.SendStatus(fiber.StatusNoContent)
}
```

---

### Layer 5: Frontend

**File: `ui/pages/project-domains.html`**
```html
<div class="space-y-6">
    <div class="flex justify-between items-center">
        <h2 class="text-2xl font-bold">Domains & SSL</h2>
        <button @click="showAddModal = true" class="btn btn-primary">+ Add Domain</button>
    </div>
    
    <div class="space-y-3">
        <template x-for="d in domains || []" :key="d.id">
            <div class="card bg-white p-4">
                <div class="flex justify-between items-start">
                    <div>
                        <h3 class="font-bold" x-text="d.subdomain ? d.subdomain + '.' + d.domain_name : d.domain_name"></h3>
                        <p class="text-sm text-gray-600">Port: <span x-text="d.container_port"></span> | 
                            Status: <span class="badge" :class="d.status === 'active' ? 'badge-success' : 'badge-warning'" x-text="d.status"></span>
                        </p>
                    </div>
                    <button @click="deleteDomain(d.id)" class="btn btn-sm btn-ghost">Delete</button>
                </div>
            </div>
        </template>
    </div>
    
    <!-- Add Domain Modal -->
    <div x-show="showAddModal" class="modal modal-open">
        <div class="modal-box">
            <h3 class="font-bold text-lg">Add Domain</h3>
            <div class="form-control space-y-4">
                <input type="text" placeholder="example.com" x-model="form.domain_name" class="input input-bordered">
                <input type="text" placeholder="api (optional)" x-model="form.subdomain" class="input input-bordered">
                <input type="number" placeholder="8080" x-model.number="form.container_port" class="input input-bordered">
            </div>
            <div class="modal-action">
                <button @click="showAddModal = false" class="btn">Cancel</button>
                <button @click="addDomain()" class="btn btn-primary">Add</button>
            </div>
        </div>
    </div>
</div>

<script>
function createDomainTab() {
    return {
        domains: [],
        showAddModal: false,
        form: { domain_name: '', subdomain: '', container_port: 8080 },
        
        async loadDomains() {
            const res = await fetch(`/api/projects/${projectId}/domains`);
            this.domains = (await res.json()).domains || [];
        },
        
        async addDomain() {
            await fetch(`/api/projects/${projectId}/domains`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(this.form)
            });
            this.showAddModal = false;
            this.form = { domain_name: '', subdomain: '', container_port: 8080 };
            await this.loadDomains();
        },
        
        async deleteDomain(id) {
            if (confirm('Delete domain?')) {
                await fetch(`/api/projects/${projectId}/domains/${id}`, { method: 'DELETE' });
                await this.loadDomains();
            }
        }
    };
}
</script>
```

---

## Implementation Waves

### Wave 1: Data Layer (6 pts)
- [ ] Migration 005: domains, ssl_certificates, proxy_configs tables
- [ ] Domain & SSL models + enums
- [ ] Repositories (CRUD operations)

**Owner:** Backend | **Duration:** 1 day

### Wave 2: ACME & Certificates (6 pts)
- [ ] ACME client (HTTP-01 via `lego`)
- [ ] Self-signed cert generation
- [ ] Certificate storage (encrypted)
- [ ] Renewal scheduler

**Owner:** Backend | **Duration:** 1.5 days

### Wave 3: Domain Service, API & UI (8 pts)
- [ ] DomainService (create, list, delete)
- [ ] Nginx config generation
- [ ] API endpoints (POST/GET/DELETE)
- [ ] Frontend domain management page

**Owner:** Backend + Frontend | **Duration:** 2 days

---

## Ticket Breakdown

| # | Ticket | Task | Points | Wave |
|---|--------|------|--------|------|
| 1 | DB-001 | Migration: domain/SSL/proxy tables | 2 | 1 |
| 2 | DOMAIN-001 | Domain & SSL models + enums | 2 | 1 |
| 3 | DOMAIN-002 | Repositories (CRUD) | 2 | 1 |
| 4 | ACME-001 | ACME client HTTP-01 | 3 | 2 |
| 5 | ACME-002 | Self-signed cert generation | 1 | 2 |
| 6 | ACME-003 | Renewal scheduler + storage | 2 | 2 |
| 7 | SVC-001 | DomainService | 4 | 3 |
| 8 | PROXY-001 | Nginx config generation | 2 | 3 |
| 9 | API-001 | Domain API endpoints | 2 | 3 |
| 10 | UI-001 | Domain management UI | 3 | 3 |
| 11 | TEST-001 | Unit & integration tests | 2 | 2-3 |

---

## Key Design

**HTTP-01 ACME:**
- Trangly listens on `:80` during certificate request
- Let's Encrypt validates by accessing `http://domain/.well-known/acme-challenge/`
- After validation, service continues normally

**Inline Nginx Config:**
- Generated per domain
- Mounted into deployment container via `docker-compose.yml` volume
- No separate Nginx container — keeps architecture simple

**Encryption at Rest:**
- All certificate keys encrypted with AES-256-GCM
- Never logged or exposed in API responses

**Certificate Renewal:**
- Daily scheduler checks for certs expiring in 30 days
- Auto-renews via ACME before expiry
- Falls back to self-signed if ACME fails

---

## Success Criteria

- [ ] Database migrations run
- [ ] Domain can be created via API
- [ ] SSL certificate auto-generated via ACME
- [ ] Certificate fallback to self-signed on ACME failure
- [ ] Nginx config generated correctly
- [ ] Certificate renewal scheduler runs daily
- [ ] Frontend allows add/remove/view domains
- [ ] All tests pass
- [ ] No unencrypted secrets in logs

---

## Out of Scope

- DNS management (user configures DNS manually)
- DNS provider integrations
- Wildcard certificates
- Certificate pinning
- Per-project domain isolation (RBAC)
- Rate limiting
- Slack/email notifications
