package enrich

import "net"

// Enricher looks up metadata for an IP and returns a fixed set of label key/value pairs.
// On any failure (nil IP, DB miss, read error) it must still return all its keys with value "unknown".
type Enricher interface {
	Name() string
	Labels(ip net.IP) map[string]string
	Close() error
}

// MMDBPaths holds the filesystem paths to the GeoLite2 databases.
type MMDBPaths struct {
	City string
	ASN  string
}

// Constructor opens an Enricher backed by the given MMDB paths.
type Constructor func(MMDBPaths) (Enricher, error)

// Registry maps enricher type names to their constructors.
type Registry map[string]Constructor

// DefaultRegistry contains the built-in enricher types.
var DefaultRegistry = Registry{
	"geoip_city": NewCityEnricher,
	"geoip_asn":  NewASNEnricher,
}

// KnownTypes returns a set of registered enricher type names, suitable for config validation.
func (r Registry) KnownTypes() map[string]bool {
	m := make(map[string]bool, len(r))
	for k := range r {
		m[k] = true
	}
	return m
}
