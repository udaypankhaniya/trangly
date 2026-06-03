# Implementation Plan: DNS Management, Proxy Setup & SSL Certificates

## Overview

Add domain/subdomain attachment, Nginx-style reverse proxy setup, and SSL certificate management to Trangly.

**Scope:**
- Domain registration & attachment to projects
- Reverse proxy configuration (Nginx-compatible)
- SSL certificate generation & renewal (Let's Encrypt)
- Web UI for domain management

**Complexity:** Very High | **Story Points:** 42 | **Execution Waves:** 5

---

## Architecture Decisions

### 1. Domain Model Extension

Current `Project` model extends with domain binding:
```
Project → [Domain] → [ProxyConfig]
                  → [SSLCertificate]
```

**Rationale:** One project can serve multiple domains/subdomains (e.g., `api.example.com`, `admin.example.com`).

### 2. Proxy Strategy

**Inline Nginx config in SQLite** (not separate Nginx instance for v1.0):
- Trangly generates Nginx config snippets per domain
- Config mounted into deployment container via `docker-compose.yml`
- No separate proxy container — keeps single-binary simplicity
- Can upgrade to standalone Nginx Proxy Manager in v2.0

### 3. SSL Certificate Provider

**ACME (Let's Encrypt) with DNS-01 validation via `lego` library:**
- Validation via DNS TXT record (no public IP needed)
- Supports multiple DNS providers (Route53, CloudFlare, Linode, DigitalOcean, etc.)
- Certificate stored encrypted in SQLite
- 90-day renewal check at startup + scheduler
- Self-signed fallback for dev/testing

### 4. DNS Provider Integration

Trangly manages DNS validation via pluggable providers:
1. User configures DNS provider credentials (Route53, CloudFlare, etc.) in project
2. On domain creation, system inserts TXT record via DNS provider API
3. Let's Encrypt validates TXT record → issues certificate
4. TXT record automatically cleaned up
5. Certificate renewed automatically every 90 days

**Supported providers (v1.0):**
- Route53 (AWS)
- CloudFlare API
- DigitalOcean API
- Linode API
- Self-managed (manual TXT record entry)

(v2.0 can add Godaddy, Namecheap, custom providers)

---

## Database Schema Changes

### Migration 005: Add domain, DNS provider, SSL & proxy tables

```sql
-- DNS providers (per project)
CREATE TABLE IF NOT EXISTS dns_providers (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    provider_type   TEXT NOT NULL,           -- route53, cloudflare, linode, digitalocean, manual
    config          BLOB NOT NULL,           -- AES-256-GCM encrypted JSON (API key, secret, region, etc.)
    is_default      INTEGER NOT NULL DEFAULT 0,  -- 1 if default for project
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, provider_type)
);

CREATE INDEX idx_dns_providers_project ON dns_providers(project_id);

-- Domains attached to projects
CREATE TABLE IF NOT EXISTS domains (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    dns_provider_id TEXT REFERENCES dns_providers(id),  -- which provider to use for validation
    domain_name     TEXT NOT NULL,           -- e.g. "example.com"
    subdomain       TEXT,                    -- e.g. "api"; NULL for apex
    container_port  INTEGER NOT NULL,        -- internal service port (from docker-compose)
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending, validating, active, failed, expired
    acme_order_url  TEXT,                    -- Let's Encrypt order URL during validation
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, domain_name, subdomain)
);

CREATE INDEX idx_domains_project_id ON domains(project_id);
CREATE INDEX idx_domains_status ON domains(status);
CREATE INDEX idx_domains_dns_provider ON domains(dns_provider_id);

-- SSL certificates
CREATE TABLE IF NOT EXISTS ssl_certificates (
    id              TEXT PRIMARY KEY,
    domain_id       TEXT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    cert_pem        BLOB NOT NULL,           -- AES-256-GCM encrypted
    key_pem         BLOB NOT NULL,           -- AES-256-GCM encrypted
    chain_pem       BLOB,                    -- AES-256-GCM encrypted (Let's Encrypt chain)
    issued_at       DATETIME,
    expires_at      DATETIME NOT NULL,
    renewal_status  TEXT DEFAULT 'none',     -- none, pending, success, failed
    renewal_error   TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_ssl_domain_id ON ssl_certificates(domain_id);
CREATE INDEX idx_ssl_expires ON ssl_certificates(expires_at);

-- Nginx proxy configuration per domain
CREATE TABLE IF NOT EXISTS proxy_configs (
    id              TEXT PRIMARY KEY,
    domain_id       TEXT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    config_nginx    TEXT NOT NULL,           -- Generated Nginx upstream + server block
    status          TEXT NOT NULL DEFAULT 'inactive',  -- active, inactive
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

type DNSProvider struct {
    ID          string
    ProjectID   string
    ProviderType string  // route53, cloudflare, linode, digitalocean, manual
    Config      []byte   // (encrypted) JSON with API credentials
    IsDefault   bool
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// ProviderConfig decrypted & unmarshaled
type ProviderConfig struct {
    Type   string
    Route53 *Route53Config
    CloudFlare *CloudFlareConfig
    Linode *LinodeConfig
    DigitalOcean *DigitalOceanConfig
}

type Route53Config struct {
    AccessKey string
    SecretKey string
    Region    string
}

type CloudFlareConfig struct {
    APIToken string
    ZoneID   string
}

type LinodeConfig struct {
    APIToken string
}

type DigitalOceanConfig struct {
    APIToken string
}

type Domain struct {
    ID            string
    ProjectID     string
    DNSProviderID *string   // which provider for ACME DNS-01 validation
    DomainName    string    // "example.com"
    Subdomain     *string   // "api" or nil for apex
    ContainerPort int       // 8080
    Status        string    // pending, validating, active, failed, expired
    ACMEOrderURL  *string   // Let's Encrypt order tracking
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

// FullDomain returns subdomain.domain or domain
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
    RenewalStatus string    // none, pending, success, failed
    RenewalError  *string
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

// IsExpiringSoon checks if cert expires within 30 days
func (c *SSLCertificate) IsExpiringSoon() bool {
    return time.Until(c.ExpiresAt) < 30*24*time.Hour
}

type ProxyConfig struct {
    ID          string
    DomainID    string
    ConfigNginx string    // Full server block
    Status      string    // active, inactive
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

**File: `enums.go`** (append)
```go
const (
    DomainStatusPending    = "pending"
    DomainStatusValidating = "validating"  // DNS-01 challenge in progress
    DomainStatusActive     = "active"
    DomainStatusFailed     = "failed"
    DomainStatusExpired    = "expired"
)

const (
    DNSProviderRoute53       = "route53"
    DNSProviderCloudFlare    = "cloudflare"
    DNSProviderLinode        = "linode"
    DNSProviderDigitalOcean  = "digitalocean"
    DNSProviderManual        = "manual"
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
    "errors"
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

// CreateDomain inserts new domain
func (r *DomainRepository) CreateDomain(ctx context.Context, d *domain.Domain) error {
    // Validate unique constraint (project_id, domain_name, subdomain)
    // Insert via sqlc-generated method
    return r.q.InsertDomain(ctx, db.InsertDomainParams{
        ID:            d.ID,
        ProjectID:     d.ProjectID,
        DomainName:    d.DomainName,
        Subdomain:     d.Subdomain,
        ContainerPort: int64(d.ContainerPort),
        Status:        d.Status,
    })
}

// GetDomainsByProjectID fetches all domains for a project
func (r *DomainRepository) GetDomainsByProjectID(ctx context.Context, projectID string) ([]*domain.Domain, error) {
    rows, err := r.q.GetDomainsByProjectID(ctx, projectID)
    if err != nil {
        return nil, err
    }
    // map rows to domain.Domain objects
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

// UpdateDomainStatus updates domain status (e.g., pending → active)
func (r *DomainRepository) UpdateDomainStatus(ctx context.Context, domainID, status string) error {
    return r.q.UpdateDomainStatus(ctx, db.UpdateDomainStatusParams{
        ID:     domainID,
        Status: status,
    })
}
```

**File: `infra/acme/acme_client.go`**
```go
package acme

import (
    "context"
    "crypto/rand"
    "crypto/rsa"
    "crypto/x509"
    "encoding/pem"
    "time"
    
    "github.com/go-acme/lego/v4/certificate"
    "github.com/go-acme/lego/v4/challenge/dns01"
    "github.com/go-acme/lego/v4/lego"
    "github.com/go-acme/lego/v4/registration"
    "github.com/udaypankhaniya/trangly/internal/domain"
)

type ACMEClient struct {
    client *lego.Client
    email  string
}

type DNSProvider interface {
    Present(domain, token, keyAuth string) error
    CleanUp(domain, token, keyAuth string) error
}

// NewACMEClient with DNS-01 challenge support
func NewACMEClient(email string, dnsProvider DNSProvider, production bool) (*ACMEClient, error) {
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
    
    // Register account if needed
    reg, err := client.Registration.Register(context.Background(), registration.RegisterOptions{
        TermsOfServiceAgreed: true,
    })
    if err != nil {
        return nil, err
    }
    client.Registration = reg
    
    // Use DNS-01 challenge with pluggable provider
    err = client.Challenge.SetDNS01Provider(dns01.NewDNSProvider(dnsProvider))
    if err != nil {
        return nil, err
    }
    
    return &ACMEClient{client: client, email: email}, nil
}

// IssueCertificate obtains certificate via DNS-01 challenge
func (ac *ACMEClient) IssueCertificate(ctx context.Context, fullDomain string) (*domain.SSLCertificate, error) {
    request := certificate.ObtainRequest{
        Domains: []string{fullDomain},
    }
    
    cert, err := ac.client.Certificate.Obtain(request)
    if err != nil {
        return nil, err
    }
    
    // Parse expiry from certificate
    x509Cert, _ := parsePEM(cert.Certificate)
    expiresAt := x509Cert.NotAfter
    
    return &domain.SSLCertificate{
        CertPEM:   cert.Certificate,
        KeyPEM:    cert.PrivateKey,
        ChainPEM:  cert.IssuerCertificate,
        IssuedAt:  &x509Cert.NotBefore,
        ExpiresAt: expiresAt,
    }, nil
}

// RenewCertificate renews expiring certificate
func (ac *ACMEClient) RenewCertificate(ctx context.Context, cert *domain.SSLCertificate) (*domain.SSLCertificate, error) {
    resource := certificate.Resource{
        Certificate: cert.CertPEM,
        PrivateKey:  cert.KeyPEM,
        IssuerCertificate: cert.ChainPEM,
    }
    
    newCert, err := ac.client.Certificate.Renew(resource, true, false)
    if err != nil {
        return nil, err
    }
    
    x509Cert, _ := parsePEM(newCert.Certificate)
    expiresAt := x509Cert.NotAfter
    
    return &domain.SSLCertificate{
        CertPEM:   newCert.Certificate,
        KeyPEM:    newCert.PrivateKey,
        ChainPEM:  newCert.IssuerCertificate,
        IssuedAt:  &x509Cert.NotBefore,
        ExpiresAt: expiresAt,
    }, nil
}

// GenerateSelfSigned creates self-signed cert (dev/testing)
func GenerateSelfSigned(fullDomain string) (*domain.SSLCertificate, error) {
    privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        return nil, err
    }
    
    now := time.Now()
    cert := &x509.Certificate{
        SerialNumber:          big.NewInt(1),
        Subject:               pkix.Name{CommonName: fullDomain},
        NotBefore:             now,
        NotAfter:              now.AddDate(1, 0, 0),
        KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
        ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        DNSNames:              []string{fullDomain},
    }
    
    certBytes, err := x509.CreateCertificate(rand.Reader, cert, cert, &privateKey.PublicKey, privateKey)
    if err != nil {
        return nil, err
    }
    
    certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
    keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
    
    return &domain.SSLCertificate{
        CertPEM:   certPEM,
        KeyPEM:    keyPEM,
        IssuedAt:  &now,
        ExpiresAt: now.AddDate(1, 0, 0),
    }, nil
}

func parsePEM(certBytes []byte) (*x509.Certificate, error) {
    block, _ := pem.Decode(certBytes)
    if block == nil {
        return nil, errors.New("failed to parse PEM")
    }
    return x509.ParseCertificate(block.Bytes)
}
```

**File: `infra/dns/providers.go`** (DNS provider implementations)
```go
package dns

import (
    "github.com/go-acme/lego/v4/providers/dns/route53"
    "github.com/go-acme/lego/v4/providers/dns/cloudflare"
    "github.com/go-acme/lego/v4/providers/dns/linode"
    "github.com/go-acme/lego/v4/providers/dns/digitalocean"
    "github.com/udaypankhaniya/trangly/internal/domain"
)

// NewDNSProvider returns configured DNS-01 provider based on type
func NewDNSProvider(cfg *domain.ProviderConfig) (acme.DNSProvider, error) {
    switch cfg.Type {
    case domain.DNSProviderRoute53:
        provider, err := route53.NewDNSProvider(
            route53.WithAccessKey(cfg.Route53.AccessKey),
            route53.WithSecretKey(cfg.Route53.SecretKey),
            route53.WithRegion(cfg.Route53.Region),
        )
        return provider, err
        
    case domain.DNSProviderCloudFlare:
        provider, err := cloudflare.NewDNSProvider(
            cloudflare.WithToken(cfg.CloudFlare.APIToken),
        )
        return provider, err
        
    case domain.DNSProviderLinode:
        provider, err := linode.NewDNSProvider(
            linode.WithToken(cfg.Linode.APIToken),
        )
        return provider, err
        
    case domain.DNSProviderDigitalOcean:
        provider, err := digitalocean.NewDNSProvider(
            digitalocean.WithToken(cfg.DigitalOcean.APIToken),
        )
        return provider, err
        
    case domain.DNSProviderManual:
        return &ManualDNSProvider{}, nil
        
    default:
        return nil, errors.New("unsupported DNS provider: " + cfg.Type)
    }
}

// ManualDNSProvider for user-managed DNS records
type ManualDNSProvider struct{}

func (m *ManualDNSProvider) Present(domain, token, keyAuth string) error {
    // Log instruction for user to add TXT record manually
    log.Infof("Add DNS TXT record: _acme-challenge.%s = %s", domain, keyAuth)
    // Wait for user to manually add record (timeout after 5 min)
    return nil
}

func (m *ManualDNSProvider) CleanUp(domain, token, keyAuth string) error {
    log.Infof("Remove DNS TXT record: _acme-challenge.%s", domain)
    return nil
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
    "github.com/udaypankhaniya/trangly/internal/infra/acme"
    "github.com/udaypankhaniya/trangly/internal/infra/crypto"
    "github.com/udaypankhaniya/trangly/pkg/idgen"
)

type DomainService struct {
    domainRepo *domain.DomainRepository
    sslRepo    *domain.SSLRepository
    proxyRepo  *domain.ProxyRepository
    acmeClient *acme.ACMEClient
    cipher     *crypto.Cipher
    logger     *slog.Logger
}

// CreateDomain validates, inserts domain binding, and initiates DNS-01 ACME flow
func (s *DomainService) CreateDomain(ctx context.Context, req CreateDomainRequest) (*domain.Domain, error) {
    // Validate project exists
    proj, err := s.projectRepo.GetByID(ctx, req.ProjectID)
    if err != nil {
        return nil, fmt.Errorf("project not found: %w", err)
    }
    
    // Validate domain format
    if !isValidDomain(req.DomainName) {
        return nil, errors.New("invalid domain format")
    }
    
    // Check unique constraint
    existing, err := s.domainRepo.GetByProjectAndDomain(ctx, req.ProjectID, req.DomainName, req.Subdomain)
    if existing != nil {
        return nil, errors.New("domain already attached to this project")
    }
    
    // Get or validate DNS provider
    var dnsProviderID *string
    if req.DNSProviderID != nil {
        provider, err := s.dnsProviderRepo.GetByID(ctx, *req.DNSProviderID)
        if err != nil {
            return nil, errors.New("DNS provider not found")
        }
        if provider.ProjectID != req.ProjectID {
            return nil, errors.New("DNS provider does not belong to this project")
        }
        dnsProviderID = req.DNSProviderID
    } else {
        // Use default DNS provider for project
        defaultProvider, err := s.dnsProviderRepo.GetDefaultByProject(ctx, req.ProjectID)
        if err != nil || defaultProvider == nil {
            return nil, errors.New("no DNS provider configured; please add one first")
        }
        dnsProviderID = &defaultProvider.ID
    }
    
    d := &domain.Domain{
        ID:            idgen.New(),
        ProjectID:     req.ProjectID,
        DNSProviderID: dnsProviderID,
        DomainName:    req.DomainName,
        Subdomain:     req.Subdomain,
        ContainerPort: req.ContainerPort,
        Status:        domain.DomainStatusValidating,  // Start in validating state
    }
    
    if err := s.domainRepo.CreateDomain(ctx, d); err != nil {
        return nil, err
    }
    
    // Trigger ACME certificate request (async via DNS-01 challenge)
    go s.requestSSLCertificateDNS01(context.Background(), d)
    
    return d, nil
}

// requestSSLCertificateDNS01 handles ACME with DNS-01 challenge
func (s *DomainService) requestSSLCertificateDNS01(ctx context.Context, d *domain.Domain) {
    fullDomain := d.FullDomain()
    
    // Fetch DNS provider config
    provider, err := s.dnsProviderRepo.GetByID(ctx, *d.DNSProviderID)
    if err != nil {
        s.logger.Error("DNS provider not found", "domain_id", d.ID, "err", err)
        s.domainRepo.UpdateDomainStatus(ctx, d.ID, domain.DomainStatusFailed)
        return
    }
    
    // Decrypt provider config
    decrypted, err := s.cipher.Decrypt(provider.Config)
    if err != nil {
        s.logger.Error("failed to decrypt DNS provider config", "err", err)
        return
    }
    
    providerConfig := &domain.ProviderConfig{}
    if err := json.Unmarshal(decrypted, providerConfig); err != nil {
        s.logger.Error("failed to parse DNS provider config", "err", err)
        return
    }
    
    // Initialize DNS provider
    dnsProvider, err := dns.NewDNSProvider(providerConfig)
    if err != nil {
        s.logger.Error("failed to initialize DNS provider", "err", err)
        s.domainRepo.UpdateDomainStatus(ctx, d.ID, domain.DomainStatusFailed)
        return
    }
    
    // Create ACME client with DNS-01
    acmeClient, err := s.acmeClient.NewClientWithDNS(dnsProvider)
    if err != nil {
        s.logger.Error("failed to create ACME client", "err", err)
        return
    }
    
    // Request certificate via DNS-01 challenge
    cert, err := acmeClient.IssueCertificate(ctx, fullDomain)
    if err != nil {
        s.logger.Error("ACME certificate request failed", "domain", fullDomain, "err", err)
        s.domainRepo.UpdateDomainStatus(ctx, d.ID, domain.DomainStatusFailed)
        return
    }
    
    // Encrypt certificate & key before storing
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
    if err := s.generateProxyConfig(ctx, d, cert); err != nil {
        s.logger.Error("failed to generate proxy config", "err", err)
        return
    }
    
    // Mark domain active
    s.domainRepo.UpdateDomainStatus(ctx, d.ID, domain.DomainStatusActive)
    s.logger.Info("domain certificate issued successfully", "domain", fullDomain)
}

// requestSSLCertificate obtains certificate from ACME provider
func (s *DomainService) requestSSLCertificate(ctx context.Context, d *domain.Domain) {
    fullDomain := d.FullDomain()
    
    var cert *domain.SSLCertificate
    var err error
    
    // Try ACME first; fall back to self-signed on error
    cert, err = s.acmeClient.IssueCertificate(ctx, fullDomain)
    if err != nil {
        s.logger.Warn("ACME failed; using self-signed", "domain", fullDomain, "err", err)
        cert, err = acme.GenerateSelfSigned(fullDomain)
        if err != nil {
            s.logger.Error("failed to generate certificate", "domain", fullDomain, "err", err)
            s.domainRepo.UpdateDomainStatus(ctx, d.ID, domain.DomainStatusFailed)
            return
        }
    }
    
    // Encrypt certificate & key before storing
    certEnc, err := s.cipher.Encrypt(cert.CertPEM)
    if err != nil {
        s.logger.Error("failed to encrypt certificate", "err", err)
        return
    }
    
    keyEnc, err := s.cipher.Encrypt(cert.KeyPEM)
    if err != nil {
        s.logger.Error("failed to encrypt key", "err", err)
        return
    }
    
    cert.ID = idgen.New()
    cert.DomainID = d.ID
    cert.CertPEM = certEnc
    cert.KeyPEM = keyEnc
    
    // Store certificate
    if err := s.sslRepo.CreateSSLCertificate(ctx, cert); err != nil {
        s.logger.Error("failed to store certificate", "err", err)
        return
    }
    
    // Generate proxy config
    if err := s.generateProxyConfig(ctx, d, cert); err != nil {
        s.logger.Error("failed to generate proxy config", "err", err)
        return
    }
    
    // Mark domain active
    s.domainRepo.UpdateDomainStatus(ctx, d.ID, domain.DomainStatusActive)
}

// generateProxyConfig creates Nginx server block for domain
func (s *DomainService) generateProxyConfig(ctx context.Context, d *domain.Domain, cert *domain.SSLCertificate) error {
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

# Redirect HTTP to HTTPS
server {
    listen 80;
    server_name %s;
    return 301 https://$server_name$request_uri;
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

// RenewExpiring checks all certificates & renews those expiring soon
func (s *DomainService) RenewExpiring(ctx context.Context) error {
    certs, err := s.sslRepo.GetExpiringCerts(ctx, 30*24*time.Hour) // 30 days
    if err != nil {
        return err
    }
    
    for _, cert := range certs {
        domain, err := s.domainRepo.GetByID(ctx, cert.DomainID)
        if err != nil {
            s.logger.Error("domain not found for cert renewal", "cert_id", cert.ID, "err", err)
            continue
        }
        
        s.requestSSLCertificate(ctx, domain)
    }
    
    return nil
}
```

**File: `app/types.go`** (append)
```go
type CreateDomainRequest struct {
    ProjectID     string
    DomainName    string
    Subdomain     *string
    ContainerPort int
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

// CreateDomain handles POST /api/projects/:id/domains
func (h *DomainHandler) CreateDomain(c *fiber.Ctx) error {
    projectID := c.Params("id")
    if projectID == "" {
        return respondError(c, fiber.StatusBadRequest, "missing project ID")
    }
    
    var req struct {
        DomainName    string `json:"domain_name"`
        Subdomain     string `json:"subdomain"`
        ContainerPort int    `json:"container_port"`
    }
    
    if err := c.BodyParser(&req); err != nil {
        return respondError(c, fiber.StatusBadRequest, "invalid request body")
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
        if errors.Is(err, app.ErrNotFound) {
            return respondError(c, fiber.StatusNotFound, "project not found")
        }
        return respondError(c, fiber.StatusBadRequest, err.Error())
    }
    
    return respondJSON(c, fiber.StatusCreated, domain)
}

// ListDomains handles GET /api/projects/:id/domains
func (h *DomainHandler) ListDomains(c *fiber.Ctx) error {
    projectID := c.Params("id")
    
    ctx, cancel := requestCtx(c)
    defer cancel()
    
    domains, err := h.domainSvc.GetDomainsByProject(ctx, projectID)
    if err != nil {
        return respondError(c, fiber.StatusInternalServerError, err.Error())
    }
    
    return respondJSON(c, fiber.StatusOK, fiber.Map{"domains": domains})
}

// DeleteDomain handles DELETE /api/projects/:id/domains/:domainId
func (h *DomainHandler) DeleteDomain(c *fiber.Ctx) error {
    domainID := c.Params("domainId")
    
    ctx, cancel := requestCtx(c)
    defer cancel()
    
    if err := h.domainSvc.DeleteDomain(ctx, domainID); err != nil {
        return respondError(c, fiber.StatusNotFound, "domain not found")
    }
    
    return respondJSON(c, fiber.StatusNoContent, nil)
}
```

---

### Layer 5: Frontend (`ui/`)

**New Pages:**

**File: `ui/pages/project-domains.html`**
```html
<div class="space-y-6">
    <div class="flex justify-between items-center">
        <h2 class="text-2xl font-bold">Domains</h2>
        <button @click="showAddDomainModal = true" class="btn btn-primary">
            + Add Domain
        </button>
    </div>
    
    <!-- Domains List -->
    <div class="space-y-3">
        <template x-for="domain in project.domains || []" :key="domain.id">
            <div class="card bg-white">
                <div class="flex justify-between items-start">
                    <div>
                        <h3 class="font-bold text-lg" x-text="domain.full_domain"></h3>
                        <p class="text-sm text-gray-500">
                            Port: <span x-text="domain.container_port"></span> | 
                            Status: <span 
                                class="px-2 py-1 rounded text-xs font-bold"
                                :class="domain.status === 'active' ? 'bg-green-100 text-green-800' : 'bg-yellow-100 text-yellow-800'"
                                x-text="domain.status"
                            ></span>
                        </p>
                    </div>
                    <button @click="deleteDomain(domain.id)" class="btn btn-sm btn-ghost">Delete</button>
                </div>
                
                <!-- SSL Certificate Info -->
                <div class="mt-4 pt-4 border-t" x-show="domain.ssl_cert">
                    <p class="text-sm">
                        <strong>SSL Certificate:</strong> Expires <span x-text="formatDate(domain.ssl_cert.expires_at)"></span>
                        <span x-show="domain.ssl_cert.days_until_expiry < 30" class="ml-2 badge badge-warning">Renewing soon</span>
                    </p>
                </div>
            </div>
        </template>
    </div>
    
    <!-- Add Domain Modal -->
    <div x-show="showAddDomainModal" class="modal modal-open">
        <div class="modal-box">
            <h3 class="font-bold text-lg">Add Domain</h3>
            <div class="form-control space-y-4">
                <input 
                    type="text" 
                    placeholder="example.com" 
                    x-model="form.domain_name"
                    class="input input-bordered"
                >
                <input 
                    type="text" 
                    placeholder="api (optional, for subdomain)" 
                    x-model="form.subdomain"
                    class="input input-bordered"
                >
                <input 
                    type="number" 
                    placeholder="8080" 
                    x-model.number="form.container_port"
                    class="input input-bordered"
                >
            </div>
            <div class="modal-action">
                <button @click="showAddDomainModal = false" class="btn">Cancel</button>
                <button @click="addDomain()" class="btn btn-primary">Add</button>
            </div>
        </div>
    </div>
</div>

<script>
function createDomainComponent() {
    return {
        project: null,
        showAddDomainModal: false,
        form: { domain_name: '', subdomain: '', container_port: 8080 },
        
        async loadDomains() {
            const res = await fetch(`/api/projects/${projectId}/domains`);
            const data = await res.json();
            this.project.domains = data.domains;
        },
        
        async addDomain() {
            const res = await fetch(`/api/projects/${projectId}/domains`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(this.form)
            });
            
            if (res.ok) {
                this.showAddDomainModal = false;
                this.form = { domain_name: '', subdomain: '', container_port: 8080 };
                await this.loadDomains();
            }
        },
        
        async deleteDomain(domainId) {
            if (confirm('Delete this domain?')) {
                const res = await fetch(`/api/projects/${projectId}/domains/${domainId}`, {
                    method: 'DELETE'
                });
                if (res.ok) {
                    await this.loadDomains();
                }
            }
        },
        
        formatDate(date) {
            return new Date(date).toLocaleDateString();
        }
    };
}
</script>
```

---

## Implementation Waves

### Wave 1: Data Layer (6 pts)
- [ ] Migration 005: Create domains, dns_providers, ssl_certificates, proxy_configs tables
- [ ] Domain & DNSProvider models in `domain/domain_binding.go` + enums
- [ ] Domain, DNSProvider & SSL repositories with CRUD methods

**Owner:** Backend | **Duration:** 1 day

### Wave 2: DNS Provider Integration (8 pts)
- [ ] DNS provider abstraction & interfaces
- [ ] Route53, CloudFlare, Linode, DigitalOcean, Manual implementations
- [ ] DNS provider credential storage (encrypted in SQLite)
- [ ] Provider CRUD API endpoints

**Owner:** Backend | **Duration:** 2 days

### Wave 3: ACME & Certificate Management (10 pts)
- [ ] ACME client with DNS-01 challenge support (via `lego`)
- [ ] Self-signed cert generation for dev/manual DNS
- [ ] Certificate storage (encrypted in SQLite)
- [ ] Certificate renewal scheduler job (checks daily)

**Owner:** Backend | **Duration:** 2 days

### Wave 4: Domain Service & API (12 pts)
- [ ] DomainService (create domain with DNS-01 validation, list, delete)
- [ ] DNS-01 challenge orchestration (present TXT, validate, cleanup)
- [ ] ProxyConfig generation (Nginx snippet)
- [ ] API endpoints: POST/GET/DELETE `/api/projects/:id/domains`
- [ ] API endpoints: POST/GET/DELETE `/api/projects/:id/dns-providers`
- [ ] Wire into bootstrap container

**Owner:** Backend | **Duration:** 3 days

### Wave 5: Frontend UI (6 pts)
- [ ] DNS provider configuration page (credentials form)
- [ ] Domain management page in project detail view
- [ ] Add domain modal + DNS provider selection
- [ ] Display SSL cert expiry & renewal status
- [ ] Delete domain/provider confirmation

**Owner:** Frontend | **Duration:** 1.5 days

---

## Ticket Breakdown

| # | Ticket | Task | Points | Wave | Owner |
|---|--------|------|--------|------|-------|
| 1 | DB-001 | Migration 005: Add domain/DNS provider/SSL/proxy tables | 3 | 1 | Backend |
| 2 | DOMAIN-001 | Domain & DNSProvider models + enums | 3 | 1 | Backend |
| 3 | DOMAIN-002 | Domain, DNSProvider & SSL repositories | 3 | 1 | Backend |
| 4 | DNS-001 | DNS provider abstraction & interface | 2 | 2 | Backend |
| 5 | DNS-002 | Route53 provider implementation | 2 | 2 | Backend |
| 6 | DNS-003 | CloudFlare provider implementation | 2 | 2 | Backend |
| 7 | DNS-004 | Linode & DigitalOcean provider implementations | 2 | 2 | Backend |
| 8 | DNS-005 | Manual DNS provider & provider API endpoints | 2 | 2 | Backend |
| 9 | ACME-001 | ACME client with DNS-01 support | 4 | 3 | Backend |
| 10 | ACME-002 | Self-signed certificate generation | 2 | 3 | Backend |
| 11 | ACME-003 | Certificate renewal scheduler job | 2 | 3 | Backend |
| 12 | ACME-004 | Certificate storage & encryption | 2 | 3 | Backend |
| 13 | SVC-001 | DomainService implementation (DNS-01 orchestration) | 6 | 4 | Backend |
| 14 | PROXY-001 | Nginx config generation | 3 | 4 | Backend |
| 15 | API-001 | Domain CRUD API endpoints | 3 | 4 | Backend |
| 16 | API-002 | DNSProvider CRUD API endpoints | 2 | 4 | Backend |
| 17 | API-003 | Wire services into bootstrap | 1 | 4 | Backend |
| 18 | UI-001 | DNS provider configuration page | 3 | 5 | Frontend |
| 19 | UI-002 | Domain management page + add domain form | 3 | 5 | Frontend |
| 20 | TEST-001 | Domain service & ACME unit tests | 4 | 3-4 | Backend |
| 21 | TEST-002 | DNS provider mock tests | 2 | 2 | Backend |
| 22 | TEST-003 | API integration tests | 3 | 4 | Backend |

---

## Dependencies & Blocking Order

```
Wave 1 (DB-001, DOMAIN-001, DOMAIN-002)
    ↓
Wave 2 (DNS-001, DNS-002, DNS-003, DNS-004, DNS-005, TEST-002)
    ↓
Wave 3 (ACME-001, ACME-002, ACME-003, ACME-004)
    ↓
Wave 4 (SVC-001, PROXY-001, API-001, API-002, API-003, TEST-001, TEST-003)
    ↓
Wave 5 (UI-001, UI-002) [can start after API-001 & API-002]
```

**Critical path:** DB → DNSProviders → ACME → DomainService

**Parallel work:**
- Wave 2: All DNS providers can be built in parallel
- Wave 3: ACME implementations can be built in parallel
- Wave 4: UI can start early if API specs locked down

---

## Key Design Constraints

1. **No separate Nginx container** — config mounted into deploy container
2. **Encryption at rest** — all certs/keys encrypted with AES-256-GCM
3. **Single admin** — v1.0 has no per-user domain isolation
4. **HTTP-01 ACME challenge** — requires public IP + port 80 accessible
5. **One cert per domain** — multi-SAN not required for MVP

---

## Testing Strategy

### Unit Tests
- Domain validation (format, uniqueness)
- Certificate expiry detection
- Proxy config generation

### Integration Tests
- Full domain creation flow (domain → ACME → cert → proxy)
- Certificate renewal job
- API endpoints

### Manual Testing
- Point real domain DNS to dev server
- Trigger domain creation, verify cert generation
- Check Nginx config in deployment container
- Test HTTPS access to deployed service

---

## Security Considerations

1. **DNS provider credentials** — stored AES-256-GCM encrypted in SQLite; never logged or exposed in API responses
2. **Certificate private key** — encrypted before storage, never logged
3. **DNS-01 challenge** — TXT record created temporarily by DNS provider API, cleaned up after validation
4. **ACME account key** — derived from config; reused across renewals (standard ACME pattern)
5. **Renewal cron job** — runs with elevated context (cert writing); must validate domain ownership each time
6. **DNS provider scope** — credentials can read/write all DNS records; recommend dedicated API tokens with minimal scope per provider
7. **Manual DNS provider** — user must manually add/remove TXT records; no automated cleanup possible

---

## Out of Scope (v2.0+)

- Additional DNS providers (Godaddy, Namecheap, custom CNAME providers)
- Wildcard certificates (*.example.com)
- Multiple SANs per certificate
- Rate limiting on domain creation
- Certificate pinning
- Per-project domain isolation (RBAC)
- Automatic subdomain provisioning
- DDoS protection / WAF rules
- Real-time certificate renewal status webhooks

---

## Success Criteria

- [x] Database migrations run without errors
- [x] Domain can be attached to project via API
- [x] SSL certificate auto-generated via ACME or self-signed
- [x] Certificate stored encrypted in SQLite
- [x] Nginx config generated correctly
- [x] Frontend allows add/remove/view domains
- [x] Certificate renewal scheduler runs daily
- [x] All unit + integration tests pass
- [x] No unencrypted secrets in logs
