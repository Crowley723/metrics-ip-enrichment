package enrich

import (
	"fmt"
	"net"

	"github.com/oschwald/geoip2-golang"
)

type asnEnricher struct {
	paths MMDBPaths
}

func NewASNEnricher(paths MMDBPaths) (Enricher, error) {
	db, err := geoip2.Open(paths.ASN)
	if err != nil {
		return nil, err
	}
	db.Close()
	return &asnEnricher{paths: paths}, nil
}

func (e *asnEnricher) Name() string { return "geoip_asn" }

func (e *asnEnricher) Labels(ip net.IP) map[string]string {
	unknown := map[string]string{
		"asn":     "unknown",
		"asn_org": "unknown",
	}
	if ip == nil {
		return unknown
	}
	db, err := geoip2.Open(e.paths.ASN)
	if err != nil {
		return unknown
	}
	defer db.Close()

	record, err := db.ASN(ip)
	if err != nil || record.AutonomousSystemNumber == 0 {
		return unknown
	}
	org := record.AutonomousSystemOrganization
	if org == "" {
		org = "unknown"
	}
	return map[string]string{
		"asn":     fmt.Sprintf("%d", record.AutonomousSystemNumber),
		"asn_org": org,
	}
}

func (e *asnEnricher) Close() error { return nil }
