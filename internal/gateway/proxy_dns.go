package gateway

import "slices"

// Add proxy domains to the bound task without changing manually managed hosts.
func includeProxyDNSHosts(c Config) Config {
	c.DDNS.Groups = slices.Clone(c.DDNS.Groups)
	for i, dns := range c.DDNS.Groups {
		hosts := slices.Clone(dns.Hosts)
		for _, group := range c.Groups {
			if group.DDNSGroupID != dns.ID {
				continue
			}
			for _, route := range c.Routes {
				if route.GroupID == group.ID && !slices.Contains(hosts, route.Host) {
					hosts = append(hosts, route.Host)
				}
			}
		}
		c.DDNS.Groups[i].Hosts = hosts
	}
	return c
}
