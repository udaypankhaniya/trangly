// Package system provides host-level introspection: OS detection, memory reading,
// and preflight checks. All three files in this package are independent of each other.
package system

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Distro represents the parsed content of /etc/os-release.
type Distro struct {
	ID      string // e.g. "ubuntu", "debian"
	Name    string // e.g. "Ubuntu 22.04.3 LTS"
	Version string // e.g. "22.04"
}

// supportedDistros lists the distro IDs supported by get.docker.com.
var supportedDistros = map[string]bool{
	"ubuntu": true,
	"debian": true,
	"centos": true,
	"fedora": true,
	"rhel":   true,
}

// ParseOSRelease reads and parses /etc/os-release.
// Returns an error only if the file cannot be opened.
func ParseOSRelease() (Distro, error) {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return Distro{}, fmt.Errorf("distro: open /etc/os-release: %w", err)
	}
	defer f.Close()

	d := Distro{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"`)
		switch k {
		case "ID":
			d.ID = strings.ToLower(v)
		case "PRETTY_NAME":
			d.Name = v
		case "VERSION_ID":
			d.Version = v
		}
	}
	return d, sc.Err()
}

// IsSupported reports whether the distro is supported by the automatic Docker installer.
func (d Distro) IsSupported() bool {
	return supportedDistros[d.ID]
}
