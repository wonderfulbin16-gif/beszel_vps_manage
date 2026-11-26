// Package geoip identifies a system's approximate IP region using a local MMDB file.
package geoip

import (
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/oschwald/maxminddb-golang/v2"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

const (
	databaseEnv = "BESZEL_GEOIP_DB"
	// Match the systems.ip_region text field.
	maxRegionLength = 120
)

var (
	countryNames = display.Regions(language.SimplifiedChinese)
	sharedIPv4   = netip.MustParsePrefix("100.64.0.0/10")
)

// Database wraps the reader so shutdown can safely close its memory map.
type Database struct {
	mu     sync.RWMutex
	reader *maxminddb.Reader
}

type placeNames map[string]string

func (n placeNames) preferred() string {
	for _, locale := range []string{"zh-CN", "zh", "en"} {
		if value := strings.TrimSpace(n[locale]); value != "" {
			return value
		}
	}
	return ""
}

type cityRecord struct {
	Country struct {
		ISOCode string     `maxminddb:"iso_code"`
		Names   placeNames `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		Names placeNames `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	City struct {
		Names placeNames `maxminddb:"names"`
	} `maxminddb:"city"`
}

// OpenConfigured opens the configured database. A nil database means geolocation is disabled.
func OpenConfigured() (*Database, error) {
	path := strings.TrimSpace(os.Getenv(databaseEnv))
	if path == "" {
		return nil, nil
	}
	reader, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open IP region database (%s): %w", databaseEnv, err)
	}
	return &Database{reader: reader}, nil
}

func (d *Database) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.reader == nil {
		return nil
	}
	err := d.reader.Close()
	d.reader = nil
	return err
}

// Lookup returns an empty region for an invalid, non-public or unmatched IP.
func (d *Database) Lookup(ip string) (string, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return "", nil
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || sharedIPv4.Contains(addr) {
		return "", nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.reader == nil {
		return "", nil
	}
	var city cityRecord
	if err := d.reader.Lookup(addr).Decode(&city); err != nil {
		return "", fmt.Errorf("look up IP region: %w", err)
	}
	return city.region(), nil
}

func (r cityRecord) region() string {
	country := strings.TrimSpace(r.Country.Names["zh-CN"])
	if country == "" {
		if region, err := language.ParseRegion(r.Country.ISOCode); err == nil {
			country = countryNames.Name(region)
		}
	}
	if country == "" {
		country = r.Country.Names.preferred()
	}

	parts := make([]string, 0, 3)
	for _, part := range append([]string{country}, r.localRegions()...) {
		if part == "" || slices.ContainsFunc(parts, func(existing string) bool {
			return strings.EqualFold(existing, part)
		}) {
			continue
		}
		if utf8.RuneCountInString(strings.Join(append(parts, part), " · ")) <= maxRegionLength {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " · ")
}

func (r cityRecord) localRegions() []string {
	parts := make([]string, 0, len(r.Subdivisions)+1)
	for _, subdivision := range r.Subdivisions {
		parts = append(parts, subdivision.Names.preferred())
	}
	return append(parts, r.City.Names.preferred())
}
