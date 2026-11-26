package geoip

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCityRegion(t *testing.T) {
	var city cityRecord
	city.Country.ISOCode = "US"
	city.City.Names = placeNames{"zh-CN": "纽约", "en": "New York"}
	require.Equal(t, "美国 · 纽约", city.region())

	city.Country.Names = placeNames{"zh-CN": "中国", "en": "China"}
	city.Subdivisions = append(city.Subdivisions, struct {
		Names placeNames `maxminddb:"names"`
	}{Names: placeNames{"zh-CN": "北京市", "en": "Beijing"}})
	city.City.Names = placeNames{"zh-CN": "北京市", "en": "Beijing"}
	require.Equal(t, "中国 · 北京市", city.region())

	city.City.Names = placeNames{"zh-CN": strings.Repeat("长", maxRegionLength)}
	require.Equal(t, "中国 · 北京市", city.region())
}

func TestDBIPCityLookup(t *testing.T) {
	path := os.Getenv("BESZEL_GEOIP_TEST_DB")
	if path == "" {
		t.Skip("set BESZEL_GEOIP_TEST_DB to a DB-IP City Lite database")
	}
	t.Setenv(databaseEnv, path)
	database, err := OpenConfigured()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })

	for _, ip := range []string{"8.8.8.8", "2001:4860:4860::8888", "::ffff:8.8.8.8"} {
		region, err := database.Lookup(ip)
		require.NoError(t, err, ip)
		require.NotEmpty(t, region, ip)
	}
	for _, ip := range []string{"", "invalid", "127.0.0.1", "10.0.0.1", "100.64.0.1", "fc00::1", "fe80::1", "::1", "2001:db8::1", "192.0.2.1"} {
		region, err := database.Lookup(ip)
		require.NoError(t, err, ip)
		require.Empty(t, region, ip)
	}
}
