package enrich

import (
	"net"

	"github.com/oschwald/geoip2-golang"
)

type cityEnricher struct {
	paths MMDBPaths
}

func NewCityEnricher(paths MMDBPaths) (Enricher, error) {
	// Verify the file opens at construction time (called each cycle).
	db, err := geoip2.Open(paths.City)
	if err != nil {
		return nil, err
	}
	db.Close()
	return &cityEnricher{paths: paths}, nil
}

func (e *cityEnricher) Name() string { return "geoip_city" }

func (e *cityEnricher) Labels(ip net.IP) map[string]string {
	unknown := map[string]string{
		"country_code": "unknown",
		"country_name": "unknown",
	}
	if ip == nil {
		return unknown
	}
	db, err := geoip2.Open(e.paths.City)
	if err != nil {
		return unknown
	}
	defer db.Close()

	record, err := db.City(ip)
	if err != nil || record.Country.IsoCode == "" {
		return unknown
	}
	name := record.Country.Names["en"]
	if name == "" {
		name = "unknown"
	}
	return map[string]string{
		"country_code": record.Country.IsoCode,
		"country_name": name,
	}
}

func (e *cityEnricher) Close() error { return nil }
